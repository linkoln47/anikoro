package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rs/cors"
)

const ()

var ()

func main() {
	clientID := firstNonEmpty(
		strings.TrimSpace(os.Getenv("MAL_CLIENT_ID")),
	)
	clientSecret := firstNonEmpty(
		strings.TrimSpace(os.Getenv("MAL_CLIENT_SECRET")),
	)

	db, err := openDB()
	if err != nil {
		logError("main", "failed to open database", "err", err)
		return
	}
	defer db.Close()

	// Setup router with CORS
	router := setupRouter(db, clientID, clientSecret)
	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://localhost:3001"}, // React dev servers
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})

	handler := c.Handler(router)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logInfo("main", "starting HTTP server", "port", port)
	logInfo(
		"main",
		"API routes configured",
		"anime", "GET /api/anime",
		"sync", "POST /api/sync",
		"stats", "GET /api/stats",
	)

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		logError("main", "HTTP server stopped with error", "err", err)
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

func writeFileWithChangeLog(path string, newContent []byte, perm os.FileMode, label string) error {
	oldContent, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logInfo("main", "creating new file", "label", label, "path", path)
			return os.WriteFile(path, newContent, perm)
		}
		return err
	}

	if string(oldContent) == string(newContent) {
		logInfo("main", "file content unchanged", "label", label, "path", path, "changes", 0)
		return os.WriteFile(path, newContent, perm)
	}

	added, removed := countLineChanges(string(oldContent), string(newContent))
	logInfo("main", "overwriting file with changes", "label", label, "path", path, fmt.Sprintf("+%d/-%d", added, removed))
	return os.WriteFile(path, newContent, perm)
}

func countLineChanges(oldText, newText string) (added int, removed int) {
	oldLines := normalizeLines(oldText)
	newLines := normalizeLines(newText)

	oldCount := make(map[string]int)
	newCount := make(map[string]int)
	for _, line := range oldLines {
		oldCount[line]++
	}
	for _, line := range newLines {
		newCount[line]++
	}

	for line, count := range newCount {
		if count > oldCount[line] {
			added += count - oldCount[line]
		}
	}
	for line, count := range oldCount {
		if count > newCount[line] {
			removed += count - newCount[line]
		}
	}
	return added, removed
}

func normalizeLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func appFilePath(name string) string {
	baseDir := strings.TrimSpace(os.Getenv("MAL_DATA_DIR"))
	if baseDir == "" {
		_, sourceFile, _, ok := runtime.Caller(0)
		if ok {
			baseDir = filepath.Dir(sourceFile)
		}
	}
	if baseDir == "" {
		wd, err := os.Getwd()
		if err == nil {
			baseDir = wd
		}
	}
	if baseDir == "" {
		return name
	}
	return filepath.Join(baseDir, name)
}
