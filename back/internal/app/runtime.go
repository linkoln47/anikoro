package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/cors"
)

var defaultCORSMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

const (
	httpServerReadHeaderTimeout = 5 * time.Second
	httpServerReadTimeout       = 30 * time.Second
	httpServerWriteTimeout      = 2 * time.Minute
	httpServerIdleTimeout       = 2 * time.Minute
	httpServerShutdownTimeout   = 15 * time.Second
)

func Main(args []string) {
	app := NewApp()
	defer func() { _ = app.Close() }()

	if len(args) > 0 {
		app.logWarn("main", "command arguments are ignored; starting HTTP server", "args", strings.Join(args, " "))
	}

	if err := app.RunHTTPServer(); err != nil {
		app.logError("main", "HTTP server stopped with error", "err", err)
	}
}

func (a *App) RunHTTPServer() error {
	if err := a.OpenDB(); err != nil {
		return err
	}
	if err := a.compose(); err != nil {
		return err
	}

	router := a.SetupRouter()
	allowedOrigins := a.Config.CORSAllowedOrigins
	handler := http.Handler(router)
	if len(allowedOrigins) > 0 {
		c := cors.New(cors.Options{
			AllowedOrigins:   allowedOrigins,
			AllowedMethods:   defaultCORSMethods,
			AllowedHeaders:   []string{"*"},
			AllowCredentials: true,
		})
		handler = c.Handler(router)
	}
	handler = withSecurityHeaders(handler)

	a.logInfo("main", "starting HTTP server", "port", a.Config.Port)
	if len(allowedOrigins) > 0 {
		a.logInfo("main", "configured CORS", "allowed_origins", strings.Join(allowedOrigins, ","), "allow_credentials", true)
	} else {
		a.logInfo("main", "CORS middleware disabled", "reason", "CORS_ALLOWED_ORIGINS is empty")
	}
	if a.Config.SessionSecret == "" {
		a.logWarn("main", "MAL_SESSION_SECRET is not set; using development session signing fallback")
	}
	a.logInfo(
		"main",
		"API routes configured",
		"auth_start", "GET /api/auth/mal/start",
		"auth_callback", "GET /api/auth/mal/callback",
		"me", "GET /api/me",
		"anime", "GET /api/anime",
		"sync", "POST /api/sync",
		"stats", "GET /api/stats",
	)

	server := &http.Server{
		Addr:              ":" + a.Config.Port,
		Handler:           handler,
		ReadHeaderTimeout: httpServerReadHeaderTimeout,
		ReadTimeout:       httpServerReadTimeout,
		WriteTimeout:      httpServerWriteTimeout,
		IdleTimeout:       httpServerIdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownSignal.Done():
		stop()
		a.logInfo("main", "shutdown signal received; stopping HTTP server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpServerShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			a.logError("main", "graceful HTTP shutdown failed; forcing close", "err", err)
			if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				return errors.Join(err, closeErr)
			}
			if serveErr := <-serverErrors; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				return errors.Join(err, serveErr)
			}
			return err
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		a.logInfo("main", "HTTP server stopped gracefully")
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
