package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/cors"
)

var defaultCORSMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}

func main() {
	app := NewApp()
	defer func() { _ = app.Close() }()

	if len(os.Args) > 1 {
		switch strings.ToLower(strings.TrimSpace(os.Args[1])) {
		case "auth":
			if err := app.runAuthCommand(); err != nil {
				app.logError("main", "failed to complete MAL authorization", "err", err)
			}
			return
		default:
			app.logWarn("main", "unknown command, starting HTTP server instead", "command", os.Args[1])
		}
	}

	if err := app.runHTTPServer(); err != nil {
		app.logError("main", "HTTP server stopped with error", "err", err)
	}
}

func (a *App) runHTTPServer() error {
	if err := a.OpenDB(); err != nil {
		return err
	}

	router := a.setupRouter()
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
	a.logInfo(
		"main",
		"API routes configured",
		"anime", "GET /api/anime",
		"sync", "POST /api/sync",
		"stats", "GET /api/stats",
	)

	return http.ListenAndServe(":"+a.Config.Port, handler)
}

func (a *App) runAuthCommand() error {
	if a.Config.ClientID == "" {
		return errors.New("MAL_CLIENT_ID is required for auth command")
	}
	if a.Config.RedirectURI == "" {
		return errors.New("MAL_REDIRECT_URI is required for auth command")
	}

	a.logInfo("main", "starting MAL authorization flow", "redirect_uri", a.Config.RedirectURI, "token_path", a.Config.TokenPath)
	token, err := a.ensureToken()
	if err != nil {
		return err
	}

	a.logInfo(
		"main",
		"MAL token ready",
		"token_path", a.Config.TokenPath,
		"expires_at", token.ExpiresAt.Format(time.RFC3339),
	)
	return nil
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
