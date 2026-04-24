package main

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDB_ListAnime_GroupsCurrentUserItemsWithoutUserGroupSnapshot(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		animeListRows().
			AddRow(20, "Series A", 8, 12, time.Now().UTC(), "Series A", "tv", int64(0), 20, "").
			AddRow(5, "Movie B", 10, 1, time.Now().UTC(), "Movie B", "movie", int64(0), 5, ""),
	)
	expectCommit(mock)

	items, err := sut.ListAnime(testUserID)
	if err != nil {
		t.Fatalf("listAnime snapshot: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("snapshot item count = %d, want 2", len(items))
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
		t.Fatalf("getStats snapshot: %v", err)
	}
	if stats != (StatsResponse{SeriesCount: 1, MoviesCount: 1, TotalCount: 2}) {
		t.Fatalf("stats = %#v, want %#v", stats, StatsResponse{SeriesCount: 1, MoviesCount: 1, TotalCount: 2})
	}
}

func TestDB_ListAnime_BuildsFranchiseFromCatalogRelationsAndUserItems(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		animeListRows().
			AddRow(10, "Series One", 9, 12, time.Now().UTC(), "Series One", "tv", int64(100), 10, "Series One").
			AddRow(20, "Series Two", 8, 12, time.Now().UTC(), "Series Two", "tv", int64(100), 10, "Series One"),
	)
	expectFranchiseMembers(mock, sqlRows("franchise_id", "anime_id").
		AddRow(int64(100), 10).
		AddRow(int64(100), 20).
		AddRow(int64(100), 30), 100)

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

func TestDB_ListAnime_UsesGlobalFranchiseMembers(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		animeListRows().
			AddRow(10, "Seeded Series", 9, 12, time.Now().UTC(), "Seeded Series", "tv", int64(100), 10, "Seeded Series"),
	)
	expectFranchiseMembers(mock, sqlRows("franchise_id", "anime_id").
		AddRow(int64(100), 10).
		AddRow(int64(100), 20).
		AddRow(int64(100), 30), 100)

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
		animeListRows().
			AddRow(10, "Alpha User", 9, 12, time.Now().UTC(), "Alpha User", "tv", int64(100), 10, "Sorted Group").
			AddRow(20, "Beta User", 8, 12, time.Now().UTC(), "Beta User", "tv", int64(100), 10, "Sorted Group"),
	)
	expectFranchiseMembers(mock, sqlRows("franchise_id", "anime_id").
		AddRow(int64(100), 10).
		AddRow(int64(100), 20).
		AddRow(int64(100), 30).
		AddRow(int64(100), 40), 100)

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

func expectAnimeList(mock sqlmock.Sqlmock, userID int64, rows *sqlmock.Rows) {
	expectUserScope(mock, userID)
	mock.ExpectQuery("SELECT\\s+ui\\.anime_id,\\s+ui\\.source_title,\\s+ui\\.score,\\s+ui\\.watched_episodes,\\s+ui\\.synced_at").
		WithArgs(userID).
		WillReturnRows(rows)
}

func animeListRows() *sqlmock.Rows {
	return sqlRows(
		"anime_id",
		"source_title",
		"score",
		"watched_episodes",
		"synced_at",
		"catalog_title",
		"media_type",
		"franchise_id",
		"representative_anime_id",
		"franchise_display_title",
	)
}

func expectFranchiseMembers(mock sqlmock.Sqlmock, rows *sqlmock.Rows, franchiseIDs ...int64) {
	args := make([]driver.Value, 0, len(franchiseIDs))
	for _, franchiseID := range franchiseIDs {
		args = append(args, franchiseID)
	}

	mock.ExpectQuery("SELECT franchise_id, anime_id\\s+FROM anime_franchise_members\\s+WHERE franchise_id IN").
		WithArgs(args...).
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
	args := make([]driver.Value, 0, len(animeIDs)+1)
	args = append(args, testUserID)
	for _, animeID := range animeIDs {
		args = append(args, animeID)
	}

	mock.ExpectQuery("SELECT anime_id, COALESCE\\(score, 0\\), watched_episodes\\s+FROM user_anime_items\\s+WHERE user_id = \\$1").
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
	rows := animeListRows()
	now := time.Now().UTC()
	for i := 0; i < seriesCount; i++ {
		animeID := 1000 + i
		rows.AddRow(animeID, "Series", 7, 12, now, "Series", "tv", int64(0), animeID, "")
	}
	for i := 0; i < movieCount; i++ {
		animeID := 2000 + i
		rows.AddRow(animeID, "Movie", 8, 1, now, "Movie", "movie", int64(0), animeID, "")
	}
	expectAnimeList(mock, userID, rows)
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
