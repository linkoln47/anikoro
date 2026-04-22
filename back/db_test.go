package main

import (
	"database/sql/driver"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDB_SaveGroupedLists_RewritesPerUserSnapshotAndExposesReadableData(t *testing.T) {
	sut, mock := newTestApp(t)

	firstSeries := []GroupedView{
		{
			ID:                 20,
			GroupKey:           "20:21",
			DisplayTitle:       "Series A",
			MergedTitles:       2,
			AvgScore:           8.5,
			WatchedEpisodesSum: 24,
		},
	}
	firstMovies := []GroupedView{
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

	if err := sut.SaveGroupedLists(testUserID, firstSeries, firstMovies); err != nil {
		t.Fatalf("saveGroupedLists first snapshot: %v", err)
	}

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(20, "series", "Series A", 2, 8.5, "{}", 24, time.Now().UTC()).
			AddRow(5, "movie", "Movie B", 1, 10.0, "{}", 1, time.Now().UTC()),
	)
	expectCommit(mock)

	items, err := sut.ListAnime(testUserID)
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

	stats, err := sut.GetStats(testUserID)
	if err != nil {
		t.Fatalf("getStats first snapshot: %v", err)
	}
	if stats != (StatsResponse{SeriesCount: 1, MoviesCount: 1, TotalCount: 2}) {
		t.Fatalf("first stats = %#v, want %#v", stats, StatsResponse{SeriesCount: 1, MoviesCount: 1, TotalCount: 2})
	}

	secondSeries := []GroupedView{
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

	if err := sut.SaveGroupedLists(testUserID, secondSeries, nil); err != nil {
		t.Fatalf("saveGroupedLists second snapshot: %v", err)
	}

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(99, "series", "Series Replacement", 1, 7.0, "{}", 13, time.Now().UTC()),
	)
	expectCommit(mock)

	items, err = sut.ListAnime(testUserID)
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

	stats, err = sut.GetStats(testUserID)
	if err != nil {
		t.Fatalf("getStats second snapshot: %v", err)
	}
	if stats != (StatsResponse{SeriesCount: 1, MoviesCount: 0, TotalCount: 1}) {
		t.Fatalf("second stats = %#v, want %#v", stats, StatsResponse{SeriesCount: 1, MoviesCount: 0, TotalCount: 1})
	}
}

func TestDB_ListAnime_BuildsFranchiseFromCatalogRelationsAndUserItems(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(10, "series", "Series One", 2, 8.5, "{10,20}", 24, time.Now().UTC()),
	)
	expectFranchiseLookup(mock, 10, sqlRows("neighbor_id").AddRow(20))
	expectFranchiseLookup(mock, 20, sqlRows("neighbor_id").AddRow(10).AddRow(30))
	expectFranchiseLookup(mock, 30, sqlRows("neighbor_id").AddRow(20))

	expectCatalogItems(mock,
		sqlRows("id", "title", "media_type", "start_date", "img_small_url", "img_large_url").
			AddRow(10, "Series One", "tv", "2020-01-01", "m1", "l1").
			AddRow(20, "Series Two", "tv", "2021-01-01", "m2", "l2").
			AddRow(30, "Movie Side Story", "movie", "2022-01-01", "m3", "l3"),
		10, 20, 30,
	)
	expectUserAnimeItems(mock,
		sqlRows("anime_id", "score", "watched_episodes").
			AddRow(10, 9, 12).
			AddRow(20, 8, 12),
		10, 20, 30,
	)
	expectRelationBatch(mock,
		sqlRows("id", "related_id", "relation_type").
			AddRow(10, 20, "sequel").
			AddRow(20, 10, "prequel").
			AddRow(20, 30, "side_story").
			AddRow(30, 20, "parent_story"),
		10, 20, 30,
	)
	expectCommit(mock)

	items, err := sut.ListAnime(testUserID)
	if err != nil {
		t.Fatalf("listAnime with franchise: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}

	got := items[0]
	if len(got.Franchise) != 3 {
		t.Fatalf("franchise item count = %d, want 3", len(got.Franchise))
	}
	if got.Franchise[0].ID != 10 || !got.Franchise[0].InUserList || got.Franchise[0].UserScore != 9 {
		t.Fatalf("first franchise item = %#v, want seeded user item", got.Franchise[0])
	}
	if got.Franchise[1].ID != 20 || !got.Franchise[1].InUserList || got.Franchise[1].WatchedEpisodes != 12 {
		t.Fatalf("second franchise item = %#v, want second user item", got.Franchise[1])
	}
	if got.Franchise[2].ID != 30 || got.Franchise[2].InUserList {
		t.Fatalf("third franchise item = %#v, want related catalog-only item", got.Franchise[2])
	}
	if got.Franchise[2].Title != "Movie Side Story" ||
		got.Franchise[2].MediaType != "movie" ||
		got.Franchise[2].StartDate != "2022-01-01" ||
		got.Franchise[2].ImageMediumURL != "m3" ||
		got.Franchise[2].ImageLargeURL != "l3" {
		t.Fatalf("third franchise item metadata = %#v, want catalog metadata", got.Franchise[2])
	}
	if got.Franchise[2].RelationType != "side_story" || got.Franchise[2].RelationTypeFormatted != "Side story" {
		t.Fatalf("third franchise item relation = %#v, want side_story metadata", got.Franchise[2])
	}
}

func TestDB_ListAnime_UsesGroupMemberIDsAsFranchiseSeeds(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(10, "series", "Seeded Series", 1, 9.0, "{10}", 12, time.Now().UTC()),
	)
	expectFranchiseLookup(mock, 10, sqlRows("neighbor_id").AddRow(20))
	expectFranchiseLookup(mock, 20, sqlRows("neighbor_id").AddRow(10).AddRow(30))
	expectFranchiseLookup(mock, 30, sqlRows("neighbor_id").AddRow(20))

	expectCatalogItems(mock,
		sqlRows("id", "title", "media_type", "start_date", "img_small_url", "img_large_url").
			AddRow(10, "Seeded Series", "tv", "2020-01-01", "m10", "l10").
			AddRow(20, "Expanded Season", "tv", "2021-01-01", "m20", "l20").
			AddRow(30, "Expanded Movie", "movie", "2022-01-01", "m30", "l30"),
		10, 20, 30,
	)
	expectUserAnimeItems(mock,
		sqlRows("anime_id", "score", "watched_episodes").
			AddRow(10, 9, 12),
		10, 20, 30,
	)
	expectRelationBatch(mock,
		sqlRows("id", "related_id", "relation_type").
			AddRow(10, 20, "sequel").
			AddRow(20, 10, "prequel").
			AddRow(20, 30, "side_story"),
		10, 20, 30,
	)
	expectCommit(mock)

	items, err := sut.ListAnime(testUserID)
	if err != nil {
		t.Fatalf("listAnime with seed expansion: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}

	gotIDs := []int{
		items[0].Franchise[0].ID,
		items[0].Franchise[1].ID,
		items[0].Franchise[2].ID,
	}
	wantIDs := []int{10, 20, 30}
	if !equalIntSlices(gotIDs, wantIDs) {
		t.Fatalf("franchise ids = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestDB_ListAnime_SortsFranchisePredictably(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(20, "series", "Sorted Group", 2, 8.0, "{20,10}", 24, time.Now().UTC()),
	)
	expectFranchiseLookup(mock, 20, sqlRows("neighbor_id").AddRow(10).AddRow(30).AddRow(40))
	expectFranchiseLookup(mock, 10, sqlRows("neighbor_id").AddRow(20))
	expectFranchiseLookup(mock, 30, sqlRows("neighbor_id").AddRow(20))
	expectFranchiseLookup(mock, 40, sqlRows("neighbor_id").AddRow(20))

	expectCatalogItems(mock,
		sqlRows("id", "title", "media_type", "start_date", "img_small_url", "img_large_url").
			AddRow(10, "Alpha User", "tv", "2020-01-01", "m10", "l10").
			AddRow(20, "Beta User", "tv", "2021-01-01", "m20", "l20").
			AddRow(30, "Gamma Related", "movie", "2022-01-01", "m30", "l30").
			AddRow(40, "Omega Related", "movie", "", "m40", "l40"),
		10, 20, 30, 40,
	)
	expectUserAnimeItems(mock,
		sqlRows("anime_id", "score", "watched_episodes").
			AddRow(10, 9, 12).
			AddRow(20, 8, 12),
		10, 20, 30, 40,
	)
	expectRelationBatch(mock,
		sqlRows("id", "related_id", "relation_type").
			AddRow(10, 20, "sequel").
			AddRow(20, 10, "prequel").
			AddRow(20, 30, "side_story").
			AddRow(20, 40, "alternative_version"),
		10, 20, 30, 40,
	)
	expectCommit(mock)

	items, err := sut.ListAnime(testUserID)
	if err != nil {
		t.Fatalf("listAnime with sorting: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}

	got := []int{
		items[0].Franchise[0].ID,
		items[0].Franchise[1].ID,
		items[0].Franchise[2].ID,
		items[0].Franchise[3].ID,
	}
	want := []int{10, 20, 30, 40}
	if !equalIntSlices(got, want) {
		t.Fatalf("sorted franchise ids = %#v, want %#v", got, want)
	}
}

func expectSnapshotRewrite(mock sqlmock.Sqlmock, userID int64, seriesGroups, movieGroups []GroupedView) {
	type expectedEntry struct {
		id                 int
		typ                string
		displayTitle       string
		mergedTitles       int
		avgScore           float64
		groupMemberIDs     string
		watchedEpisodesSum int
	}

	entries := make([]expectedEntry, 0, len(seriesGroups)+len(movieGroups))
	for _, entry := range seriesGroups {
		entries = append(entries, expectedEntry{
			id:                 entry.ID,
			typ:                "series",
			displayTitle:       entry.DisplayTitle,
			mergedTitles:       entry.MergedTitles,
			avgScore:           entry.AvgScore,
			groupMemberIDs:     "{}",
			watchedEpisodesSum: entry.WatchedEpisodesSum,
		})
	}
	for _, entry := range movieGroups {
		entries = append(entries, expectedEntry{
			id:                 entry.ID,
			typ:                "movie",
			displayTitle:       entry.DisplayTitle,
			mergedTitles:       entry.MergedTitles,
			avgScore:           entry.AvgScore,
			groupMemberIDs:     "{}",
			watchedEpisodesSum: entry.WatchedEpisodesSum,
		})
	}

	expectUserScope(mock, userID)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM user_anime_groups")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	prepare := mock.ExpectPrepare("INSERT INTO user_anime_groups")
	for _, entry := range entries {
		prepare.ExpectExec().
			WithArgs(entry.id, entry.typ, entry.displayTitle, entry.mergedTitles, entry.avgScore, entry.groupMemberIDs, entry.watchedEpisodesSum, sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
	expectCommit(mock)
}

func expectAnimeList(mock sqlmock.Sqlmock, userID int64, rows *sqlmock.Rows) {
	expectUserScope(mock, userID)
	mock.ExpectQuery("SELECT\\s+anime_id,\\s+anime_type,\\s+display_title,\\s+merged_titles,\\s+avg_score,\\s+group_member_ids::text,\\s+watched_episodes_sum,\\s+synced_at\\s+FROM user_anime_groups").
		WillReturnRows(rows)
}

func expectFranchiseLookup(mock sqlmock.Sqlmock, animeID int, rows *sqlmock.Rows) {
	mock.ExpectQuery("SELECT DISTINCT neighbor_id").
		WithArgs(animeID).
		WillReturnRows(rows)
}

func expectCatalogItems(mock sqlmock.Sqlmock, rows *sqlmock.Rows, animeIDs ...int) {
	args := make([]driver.Value, 0, len(animeIDs))
	for _, animeID := range animeIDs {
		args = append(args, animeID)
	}

	mock.ExpectQuery("SELECT\\s+id,\\s+COALESCE\\(title, ''\\),\\s+COALESCE\\(media_type, ''\\),\\s+COALESCE\\(start_date::text, ''\\),\\s+COALESCE\\(img_small_url, ''\\),\\s+COALESCE\\(img_large_url, ''\\)\\s+FROM anime_catalog\\s+WHERE id IN").
		WithArgs(args...).
		WillReturnRows(rows)
}

func expectUserAnimeItems(mock sqlmock.Sqlmock, rows *sqlmock.Rows, animeIDs ...int) {
	args := make([]driver.Value, 0, len(animeIDs))
	for _, animeID := range animeIDs {
		args = append(args, animeID)
	}

	mock.ExpectQuery("SELECT anime_id, COALESCE\\(score, 0\\), watched_episodes\\s+FROM user_anime_items\\s+WHERE anime_id IN").
		WithArgs(args...).
		WillReturnRows(rows)
}

func expectRelationBatch(mock sqlmock.Sqlmock, rows *sqlmock.Rows, sourceIDs ...int) {
	args := make([]driver.Value, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		args = append(args, sourceID)
	}

	mock.ExpectQuery("SELECT\\s+id,\\s+related_id,\\s+COALESCE\\(relation_type, ''\\)\\s+FROM anime_relations\\s+WHERE id IN").
		WithArgs(args...).
		WillReturnRows(rows)
}

func expectStats(mock sqlmock.Sqlmock, userID int64, seriesCount, movieCount int) {
	expectUserScope(mock, userID)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT
				COUNT(*) FILTER (WHERE anime_type = 'series'),
				COUNT(*) FILTER (WHERE anime_type = 'movie')
			FROM user_anime_groups
		`)).
		WillReturnRows(sqlRows("series_count", "movie_count").AddRow(seriesCount, movieCount))
	expectCommit(mock)
}

func equalIntSlices(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}

	return true
}
