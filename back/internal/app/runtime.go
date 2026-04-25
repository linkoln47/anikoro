package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/cors"
)

var defaultCORSMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}

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

	return http.ListenAndServe(":"+a.Config.Port, handler)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *App) writeFileWithChangeLog(path string, newContent []byte, perm os.FileMode, label string) error {
	oldContent, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.logInfo("main", "creating new file", "label", label, "path", path)
			return writeFileAtomically(path, newContent, perm)
		}
		return err
	}

	if string(oldContent) == string(newContent) {
		a.logInfo("main", "file content unchanged", "label", label, "path", path)
		return nil
	}

	a.logInfo("main", "overwriting file", "label", label, "path", path)
	return writeFileAtomically(path, newContent, perm)
}

func writeFileAtomically(path string, content []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	pattern := filepath.Base(path) + ".tmp-*"

	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}
