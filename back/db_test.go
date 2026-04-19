package main

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSaveGroupedLists_RewritesPerUserSnapshotAndExposesReadableData(t *testing.T) {
	app, mock := newTestApp(t)

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

	expectSnapshotRewrite(mock, testUserID, firstSeries, firstMovies)

	if err := app.saveGroupedLists(testUserID, firstSeries, firstMovies); err != nil {
		t.Fatalf("saveGroupedLists first snapshot: %v", err)
	}

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "watched_episodes_sum", "synced_at").
			AddRow(20, "series", "Series A", 2, 8.5, 24, time.Now().UTC()).
			AddRow(5, "movie", "Movie B", 1, 10.0, 1, time.Now().UTC()),
	)

	items, err := app.listAnime(testUserID)
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

	expectStats(mock, testUserID, 1, 1)

	stats, err := app.getStats(testUserID)
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

	expectSnapshotRewrite(mock, testUserID, secondSeries, nil)

	if err := app.saveGroupedLists(testUserID, secondSeries, nil); err != nil {
		t.Fatalf("saveGroupedLists second snapshot: %v", err)
	}

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "watched_episodes_sum", "synced_at").
			AddRow(99, "series", "Series Replacement", 1, 7.0, 13, time.Now().UTC()),
	)

	items, err = app.listAnime(testUserID)
	if err != nil {
		t.Fatalf("listAnime second snapshot: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("second snapshot item count = %d, want 1", len(items))
	}
	if items[0].ID != 99 || items[0].Type != "series" || items[0].DisplayTitle != "Series Replacement" {
		t.Fatalf("second snapshot item = %#v, want rewritten series snapshot", items[0])
	}

	expectStats(mock, testUserID, 1, 0)

	stats, err = app.getStats(testUserID)
	if err != nil {
		t.Fatalf("getStats second snapshot: %v", err)
	}
	if stats != (StatsResponse{SeriesCount: 1, MoviesCount: 0, TotalCount: 1}) {
		t.Fatalf("second stats = %#v, want %#v", stats, StatsResponse{SeriesCount: 1, MoviesCount: 0, TotalCount: 1})
	}
}

func expectSnapshotRewrite(mock sqlmock.Sqlmock, userID int64, seriesGroups, movieGroups []groupedView) {
	entries := flattenGroupedEntries(seriesGroups, movieGroups)

	expectUserScope(mock, userID)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM " + animeEntriesTableName)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	prepare := mock.ExpectPrepare("INSERT INTO " + animeEntriesTableName)
	for _, entry := range entries {
		prepare.ExpectExec().
			WithArgs(entry.ID, entry.Type, entry.GroupKey, entry.DisplayTitle, entry.MergedTitles, entry.AvgScore, entry.WatchedEpisodesSum, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
	expectCommit(mock)
}

func expectAnimeList(mock sqlmock.Sqlmock, userID int64, rows *sqlmock.Rows) {
	expectUserScope(mock, userID)
	mock.ExpectQuery("SELECT anime_id, anime_type, display_title, merged_titles, avg_score, watched_episodes_sum, synced_at\\s+FROM " + animeEntriesTableName).
		WillReturnRows(rows)
	expectCommit(mock)
}

func expectStats(mock sqlmock.Sqlmock, userID int64, seriesCount, movieCount int) {
	expectUserScope(mock, userID)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT
				COUNT(*) FILTER (WHERE anime_type = 'series'),
				COUNT(*) FILTER (WHERE anime_type = 'movie')
			FROM anime_entries
		`)).
		WillReturnRows(sqlRows("series_count", "movie_count").AddRow(seriesCount, movieCount))
	expectCommit(mock)
}
