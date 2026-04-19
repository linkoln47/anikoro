package tests

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	backend "test/internal/app"
)

const testUserID int64 = 42

func newTestApp(t *testing.T) (*backend.App, sqlmock.Sqlmock) {
	t.Helper()

	dataDir := t.TempDir()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}

	app := backend.NewApp()
	app.Config = backend.AppConfig{
		Port:             backend.DefaultHTTPPort,
		DatabaseURL:      "postgres://test:test@localhost/test",
		DataDir:          dataDir,
		DetailsCachePath: filepath.Join(dataDir, backend.DetailsCacheName),
	}
	app.DB = db
	app.HTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: fakeTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("unexpected outbound request: %s %s", req.Method, req.URL.String())
			},
		},
	}
	app.Logger = newTestLogger()

	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet SQL expectations: %v", err)
		}
		_ = app.Close()
	})

	return app, mock
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
		Body: io.NopCloser(stringsReader(body)),
	}
}

func textHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"text/plain; charset=utf-8"},
		},
		Body: io.NopCloser(stringsReader(body)),
	}
}

func expectUserScope(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config('app.user_id', $1, true)")).
		WithArgs(strconv.FormatInt(userID, 10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectCommit(mock sqlmock.Sqlmock) {
	mock.ExpectCommit()
}

func sqlRows(columns ...string) *sqlmock.Rows {
	return sqlmock.NewRows(columns)
}

func stringsReader(value string) *stringReader {
	return &stringReader{value: value}
}

type stringReader struct {
	value string
	index int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.index >= len(r.value) {
		return 0, io.EOF
	}

	n := copy(p, r.value[r.index:])
	r.index += n
	return n, nil
}
