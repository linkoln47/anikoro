package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveGroupedLists_RewritesSnapshotAndExposesReadableData(t *testing.T) {
	app := newTestApp(t)

	firstSeries := []groupedView{
		{
			ID:                 20,
			GroupKey:           "20:21",
			DisplayTitle:       "Series A",
			MergedTitles:       2,
			AvgScore:           8.5,
			WatchedEpisodesSum: 24,
		},
	}
	firstMovies := []groupedView{
		{
			ID:                 5,
			GroupKey:           "5",
			DisplayTitle:       "Movie B",
			MergedTitles:       1,
			AvgScore:           10,
			WatchedEpisodesSum: 1,
		},
	}

	if err := app.saveGroupedLists(firstSeries, firstMovies); err != nil {
		t.Fatalf("saveGroupedLists first snapshot: %v", err)
	}

	items, err := app.listAnime()
	if err != nil {
		t.Fatalf("listAnime first snapshot: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("first snapshot item count = %d, want 2", len(items))
	}
	if items[0].ID != 20 || items[0].Type != "series" {
		t.Fatalf("first series item = %#v, want id=20 type=series", items[0])
	}
	if items[1].ID != 5 || items[1].Type != "movie" {
		t.Fatalf("first movie item = %#v, want id=5 type=movie", items[1])
	}
	for _, item := range items {
		if _, err := time.Parse(time.RFC3339, item.SyncedAt); err != nil {
			t.Fatalf("synced_at %q is not RFC3339: %v", item.SyncedAt, err)
		}
	}

	stats, err := app.getStats()
	if err != nil {
		t.Fatalf("getStats first snapshot: %v", err)
	}
	if stats != (StatsResponse{SeriesCount: 1, MoviesCount: 1, TotalCount: 2}) {
		t.Fatalf("first stats = %#v, want %#v", stats, StatsResponse{SeriesCount: 1, MoviesCount: 1, TotalCount: 2})
	}

	secondSeries := []groupedView{
		{
			ID:                 99,
			GroupKey:           "99",
			DisplayTitle:       "Series Replacement",
			MergedTitles:       1,
			AvgScore:           7,
			WatchedEpisodesSum: 13,
		},
	}

	if err := app.saveGroupedLists(secondSeries, nil); err != nil {
		t.Fatalf("saveGroupedLists second snapshot: %v", err)
	}

	items, err = app.listAnime()
	if err != nil {
		t.Fatalf("listAnime second snapshot: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("second snapshot item count = %d, want 1", len(items))
	}
	if items[0].ID != 99 || items[0].Type != "series" || items[0].DisplayTitle != "Series Replacement" {
		t.Fatalf("second snapshot item = %#v, want rewritten series snapshot", items[0])
	}

	stats, err = app.getStats()
	if err != nil {
		t.Fatalf("getStats second snapshot: %v", err)
	}
	if stats != (StatsResponse{SeriesCount: 1, MoviesCount: 0, TotalCount: 1}) {
		t.Fatalf("second stats = %#v, want %#v", stats, StatsResponse{SeriesCount: 1, MoviesCount: 0, TotalCount: 1})
	}
}

func TestOpenDB_ValidatesExpectedSchema(t *testing.T) {
	validPath := filepath.Join(t.TempDir(), dbFileName)
	createSQLiteDBFile(t, validPath, testSQLiteSchema)

	db, err := openDB(AppConfig{DBPath: validPath})
	if err != nil {
		t.Fatalf("openDB with valid schema returned error: %v", err)
	}
	_ = db.Close()

	invalidPath := filepath.Join(t.TempDir(), dbFileName)
	createSQLiteDBFile(t, invalidPath, "")

	_, err = openDB(AppConfig{DBPath: invalidPath})
	if err == nil {
		t.Fatal("expected openDB to fail for missing schema")
	}
	if !strings.Contains(err.Error(), "validate sqlite database contract") {
		t.Fatalf("openDB error %q does not mention schema validation", err)
	}
}
