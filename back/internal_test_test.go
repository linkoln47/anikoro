package main

import (
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"test/internal/domain"
)

func newInternalTestApp(t *testing.T) (*App, sqlmock.Sqlmock) {
	t.Helper()

	dataDir := t.TempDir()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}

	app := NewApp()
	app.Config = AppConfig{
		Port:             DefaultHTTPPort,
		DatabaseURL:      "postgres://test:test@localhost/test",
		DataDir:          dataDir,
		DetailsCachePath: filepath.Join(dataDir, DetailsCacheName),
	}
	app.DB = db
	app.HTTPClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: internalFakeTransport{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("unexpected outbound request: %s %s", req.Method, req.URL.String())
			},
		},
	}
	app.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet SQL expectations: %v", err)
		}
		_ = app.Close()
	})

	return app, mock
}

type internalFakeTransport struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (t internalFakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.roundTrip == nil {
		return nil, fmt.Errorf("unexpected outbound request: %s %s", req.Method, req.URL.String())
	}

	return t.roundTrip(req)
}

func internalJSONHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(internalStringsReader(body)),
	}
}

func internalTextHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"text/plain; charset=utf-8"},
		},
		Body: io.NopCloser(internalStringsReader(body)),
	}
}

func internalSQLRows(columns ...string) *sqlmock.Rows {
	return sqlmock.NewRows(columns)
}

func expectAnimeCatalogStatesByIDs(mock sqlmock.Sqlmock, rows *sqlmock.Rows, animeIDs ...int) {
	mock.ExpectQuery("SELECT\\s+id,\\s+resolved,\\s+COALESCE\\(details_synced_at, TIMESTAMPTZ 'epoch'\\)\\s+FROM anime_catalog\\s+WHERE id IN").
		WithArgs(intsToDriverValues(animeIDs)...).
		WillReturnRows(rows)
}

func expectAnimeRelationIDsBySourceIDs(mock sqlmock.Sqlmock, rows *sqlmock.Rows, animeIDs ...int) {
	mock.ExpectQuery("SELECT\\s+id,\\s+related_id\\s+FROM anime_relations\\s+WHERE id IN").
		WithArgs(intsToDriverValues(animeIDs)...).
		WillReturnRows(rows)
}

func expectUndirectedAnimeRelationIDs(mock sqlmock.Sqlmock, animeID int, relatedIDs ...int) {
	rows := internalSQLRows("neighbor_id")
	for _, relatedID := range relatedIDs {
		rows.AddRow(relatedID)
	}

	mock.ExpectQuery("SELECT DISTINCT neighbor_id").
		WithArgs(animeID).
		WillReturnRows(rows)
}

func expectAnimeCatalogMediaType(mock sqlmock.Sqlmock, animeID int, mediaType string) {
	rows := internalSQLRows("media_type")
	if mediaType != "" {
		rows.AddRow(mediaType)
	}

	mock.ExpectQuery("SELECT\\s+media_type\\s+FROM anime_catalog\\s+WHERE id = \\$1").
		WithArgs(animeID).
		WillReturnRows(rows)
}

func expectSaveAnimeCatalogDetailsBatch(t *testing.T, mock sqlmock.Sqlmock, detailsBatch []AnimeDetails) {
	t.Helper()

	detailsBatch = domain.CloneAnimeDetailsBatch(detailsBatch)
	for index := range detailsBatch {
		if len(detailsBatch[index].Related) > 0 || len(detailsBatch[index].RelatedIDs) == 0 {
			continue
		}
		related := make([]AnimeRelation, 0, len(detailsBatch[index].RelatedIDs))
		for _, relatedID := range detailsBatch[index].RelatedIDs {
			related = append(related, AnimeRelation{ID: relatedID})
		}
		detailsBatch[index].Related = related
	}

	normalized, err := normalizeAnimeCatalogDetailsBatch(detailsBatch)
	if err != nil {
		t.Fatalf("normalize details batch: %v", err)
	}
	if len(normalized) == 0 {
		return
	}
	exactOrder := len(normalized) == 1

	stubIDs := collectAnimeCatalogStubIDs(normalized)
	sourceIDs := make([]int, 0, len(normalized))
	for _, details := range normalized {
		sourceIDs = append(sourceIDs, details.ID)
	}
	sourceIDs = uniquePositiveIDs(sourceIDs)

	mock.ExpectBegin()
	stubArgs := intsToDriverValues(stubIDs)
	if !exactOrder {
		stubArgs = repeatedAnyArgs(len(stubIDs))
	}
	mock.ExpectExec("INSERT INTO anime_catalog \\(id\\)").
		WithArgs(stubArgs...).
		WillReturnResult(sqlmock.NewResult(0, int64(len(stubIDs))))

	detailsArgs := make([]driver.Value, 0, len(normalized)*8)
	if exactOrder {
		for _, details := range normalized {
			detailsArgs = append(
				detailsArgs,
				details.ID,
				details.Title,
				details.MediaType,
				nullableDate(details.StartDate),
				details.ImageMediumURL,
				details.ImageLargeURL,
				true,
				sqlmock.AnyArg(),
			)
		}
	} else {
		detailsArgs = repeatedAnyArgs(len(normalized) * 8)
	}
	mock.ExpectExec("INSERT INTO anime_catalog \\(\\s+id,\\s+title,\\s+media_type,\\s+start_date,\\s+img_small_url,\\s+img_large_url,\\s+resolved,\\s+details_synced_at,\\s+updated_at\\s+\\) VALUES").
		WithArgs(detailsArgs...).
		WillReturnResult(sqlmock.NewResult(0, int64(len(normalized))))

	deleteArgs := intsToDriverValues(sourceIDs)
	if !exactOrder {
		deleteArgs = repeatedAnyArgs(len(sourceIDs))
	}
	mock.ExpectExec("DELETE FROM anime_relations\\s+WHERE id IN").
		WithArgs(deleteArgs...).
		WillReturnResult(sqlmock.NewResult(0, int64(len(sourceIDs))))

	relationArgs := make([]driver.Value, 0)
	relationCount := 0
	for _, details := range normalized {
		for _, relation := range details.Related {
			if relation.ID <= 0 || relation.ID == details.ID {
				continue
			}
			relationCount++
			relationArgs = append(
				relationArgs,
				details.ID,
				relation.ID,
				relation.RelationType,
			)
		}
	}
	if relationCount > 0 {
		if !exactOrder {
			relationArgs = repeatedAnyArgs(relationCount * 3)
		}
		mock.ExpectExec("INSERT INTO anime_relations \\(\\s+id,\\s+related_id,\\s+relation_type\\s+\\) VALUES").
			WithArgs(relationArgs...).
			WillReturnResult(sqlmock.NewResult(0, int64(relationCount)))
	}

	mock.ExpectCommit()
}

func animeDetailsJSON(id int, mediaType string, relatedIDs ...int) string {
	related := make([]string, 0, len(relatedIDs))
	for _, relatedID := range relatedIDs {
		related = append(related, fmt.Sprintf(`{"node":{"id":%d,"title":""}}`, relatedID))
	}

	return fmt.Sprintf(
		`{"id":%d,"title":"","media_type":"%s","related_anime":[%s]}`,
		id,
		mediaType,
		strings.Join(related, ","),
	)
}

func animeIDFromRequest(t *testing.T, req *http.Request) int {
	t.Helper()

	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) == 0 {
		t.Fatalf("cannot parse anime id from path %q", req.URL.Path)
	}

	animeID, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatalf("cannot parse anime id from path %q: %v", req.URL.Path, err)
	}
	return animeID
}

func intsToDriverValues(ids []int) []driver.Value {
	values := make([]driver.Value, 0, len(ids))
	for _, id := range ids {
		values = append(values, id)
	}
	return values
}

func repeatedAnyArgs(count int) []driver.Value {
	values := make([]driver.Value, 0, count)
	for i := 0; i < count; i++ {
		values = append(values, sqlmock.AnyArg())
	}
	return values
}

func internalStringsReader(value string) *internalStringReader {
	return &internalStringReader{value: value}
}

type internalStringReader struct {
	value string
	index int
}

func (r *internalStringReader) Read(p []byte) (int, error) {
	if r.index >= len(r.value) {
		return 0, io.EOF
	}

	n := copy(p, r.value[r.index:])
	r.index += n
	return n, nil
}
