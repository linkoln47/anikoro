package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testSQLiteSchema = `
CREATE TABLE IF NOT EXISTS series_table (
    id INTEGER PRIMARY KEY,
    group_key TEXT NOT NULL,
    display_title TEXT NOT NULL,
    merged_titles INTEGER NOT NULL,
    avg_score REAL NOT NULL,
    watched_episodes_sum INTEGER NOT NULL,
    synced_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS series_table_group_key_idx
    ON series_table (group_key);

CREATE TABLE IF NOT EXISTS movie_table (
    id INTEGER PRIMARY KEY,
    group_key TEXT NOT NULL,
    display_title TEXT NOT NULL,
    merged_titles INTEGER NOT NULL,
    avg_score REAL NOT NULL,
    watched_episodes_sum INTEGER NOT NULL,
    synced_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS movie_table_group_key_idx
    ON movie_table (group_key);
`

func newTestApp(t *testing.T) *App {
	t.Helper()

	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, dbFileName)
	createSQLiteDBFile(t, dbPath, testSQLiteSchema)

	db, err := openDB(AppConfig{DBPath: dbPath})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	app := &App{
		Config: AppConfig{
			Port:             defaultHTTPPort,
			DataDir:          dataDir,
			DBPath:           dbPath,
			TokenPath:        filepath.Join(dataDir, tokenFileName),
			DetailsCachePath: filepath.Join(dataDir, detailsCacheName),
		},
		DB: db,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: fakeTransport{
				roundTrip: func(req *http.Request) (*http.Response, error) {
					return nil, fmt.Errorf("unexpected outbound request: %s %s", req.Method, req.URL.String())
				},
			},
		},
		Logger: newTestLogger(),
	}

	t.Cleanup(func() {
		_ = app.Close()
	})

	return app
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (t fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.roundTrip == nil {
		return nil, fmt.Errorf("unexpected outbound request: %s %s", req.Method, req.URL.String())
	}

	return t.roundTrip(req)
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func textHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"text/plain; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func createSQLiteDBFile(t *testing.T, path, schema string) {
	t.Helper()

	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create sqlite file: %v", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite file: %v", err)
	}
	defer db.Close()

	if strings.TrimSpace(schema) == "" {
		return
	}

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("apply sqlite schema: %v", err)
	}
}

func writeTestToken(t *testing.T, path string, token malToken) {
	t.Helper()

	body, err := json.Marshal(token)
	if err != nil {
		t.Fatalf("marshal token: %v", err)
	}

	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
}
