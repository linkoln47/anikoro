package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestBuildAnimeListEntriesGroupsAndClassifies(t *testing.T) {
	older := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	movieSyncedAt := time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC)

	entries, err := BuildAnimeListEntries([]AnimeListGroupInput{
		{
			AnimeID:               2,
			SourceTitle:           "Show S1",
			ListStatus:            string(AnimeListStatusCompleted),
			Score:                 8,
			WatchedEpisodes:       12,
			SyncedAt:              older,
			CatalogTitle:          "Show One",
			MediaType:             "tv",
			FranchiseID:           10,
			RepresentativeAnimeID: 1,
			FranchiseDisplayTitle: "Show Franchise",
		},
		{
			AnimeID:               1,
			SourceTitle:           "Show Movie",
			ListStatus:            string(AnimeListStatusCompleted),
			Score:                 0,
			WatchedEpisodes:       1,
			SyncedAt:              newer,
			CatalogTitle:          "Show Movie",
			MediaType:             AnimeMediaTypeMovie,
			FranchiseID:           10,
			RepresentativeAnimeID: 1,
			FranchiseDisplayTitle: "Show Franchise",
		},
		{
			AnimeID:         9,
			SourceTitle:     "Standalone Movie",
			ListStatus:      string(AnimeListStatusCompleted),
			Score:           7,
			WatchedEpisodes: 1,
			SyncedAt:        movieSyncedAt,
			CatalogTitle:    "Standalone Movie",
			MediaType:       AnimeMediaTypeMovie,
		},
	}, map[int64][]int{
		10: {1, 2, 3},
	})
	if err != nil {
		t.Fatalf("BuildAnimeListEntries returned error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	series := entries[0]
	if series.Item.ID != 1 || series.Item.DisplayTitle != "Show Franchise" {
		t.Fatalf("unexpected grouped series identity: %+v", series.Item)
	}
	if series.Item.Type != AnimeListItemTypeSeries {
		t.Fatalf("expected grouped entry to be series, got %q", series.Item.Type)
	}
	if series.Item.AvgScore != 8.0 {
		t.Fatalf("expected average score 8.0, got %v", series.Item.AvgScore)
	}
	if series.Item.WatchedEpisodesSum != 13 {
		t.Fatalf("expected watched episodes sum 13, got %d", series.Item.WatchedEpisodesSum)
	}
	if series.Item.MergedTitles != 2 {
		t.Fatalf("expected 2 merged titles, got %d", series.Item.MergedTitles)
	}
	if series.Item.SyncedAt != newer.Format(time.RFC3339) {
		t.Fatalf("expected latest synced_at %s, got %s", newer.Format(time.RFC3339), series.Item.SyncedAt)
	}
	if !reflect.DeepEqual(series.GroupMemberIDs, []int{1, 2}) {
		t.Fatalf("unexpected group member ids: %+v", series.GroupMemberIDs)
	}
	if !reflect.DeepEqual(series.FranchiseMemberIDs, []int{1, 2, 3}) {
		t.Fatalf("unexpected franchise member ids: %+v", series.FranchiseMemberIDs)
	}
	if series.Item.StatusCounts[string(AnimeListStatusCompleted)] != 2 {
		t.Fatalf("expected grouped completed status count 2, got %+v", series.Item.StatusCounts)
	}

	movie := entries[1]
	if movie.Item.ID != 9 || movie.Item.Type != AnimeListItemTypeMovie {
		t.Fatalf("unexpected standalone movie entry: %+v", movie.Item)
	}

	stats := CountAnimeListStats(entries)
	if stats.SeriesCount != 1 || stats.MoviesCount != 1 || stats.TotalCount != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if stats.StatusCounts[string(AnimeListStatusCompleted)] != 3 {
		t.Fatalf("expected 3 completed anime items in stats, got %+v", stats.StatusCounts)
	}
}

func TestBuildAnimeListEntriesKeepsFranchiseWholeWithMixedStatuses(t *testing.T) {
	syncedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	entries, err := BuildAnimeListEntries([]AnimeListGroupInput{
		{
			AnimeID:               1,
			SourceTitle:           "Show S1",
			ListStatus:            string(AnimeListStatusCompleted),
			Score:                 8,
			WatchedEpisodes:       12,
			SyncedAt:              syncedAt,
			CatalogTitle:          "Show S1",
			MediaType:             "tv",
			FranchiseID:           10,
			RepresentativeAnimeID: 1,
			FranchiseDisplayTitle: "Show",
		},
		{
			AnimeID:               2,
			SourceTitle:           "Show S2",
			ListStatus:            string(AnimeListStatusWatching),
			Score:                 0,
			WatchedEpisodes:       3,
			SyncedAt:              syncedAt,
			CatalogTitle:          "Show S2",
			MediaType:             "tv",
			FranchiseID:           10,
			RepresentativeAnimeID: 1,
			FranchiseDisplayTitle: "Show",
		},
	}, map[int64][]int{
		10: {1, 2},
	})
	if err != nil {
		t.Fatalf("BuildAnimeListEntries returned error: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected one whole franchise group, got %d", len(entries))
	}
	if entries[0].Item.StatusCounts[string(AnimeListStatusCompleted)] != 1 {
		t.Fatalf("expected completed count 1, got %+v", entries[0].Item.StatusCounts)
	}
	if entries[0].Item.StatusCounts[string(AnimeListStatusWatching)] != 1 {
		t.Fatalf("expected watching count 1, got %+v", entries[0].Item.StatusCounts)
	}
}

func TestBuildFranchiseEntriesEnrichesRelationsAndSorts(t *testing.T) {
	entries := BuildFranchiseEntries(
		map[int]FranchiseEntry{
			1: {ID: 1, Title: "Group Member", StartDate: "2019-01-01"},
			2: {ID: 2, Title: "Direct Sequel", StartDate: "2020-01-01"},
			3: {ID: 3, Title: "No Date"},
			4: {ID: 4, Title: "Spin Off", StartDate: "2018-01-01"},
		},
		map[int]AnimeUserListState{
			1: {Score: 9, WatchedEpisodes: 12, ListStatus: string(AnimeListStatusCompleted)},
			2: {Score: 8, WatchedEpisodes: 10, ListStatus: string(AnimeListStatusWatching)},
		},
		map[int]map[int]AnimeRelation{
			1: {
				2: {ID: 2, RelationType: "sequel"},
			},
			2: {
				4: {ID: 4, RelationType: "spin_off"},
			},
		},
		[]int{1, 2},
		[]int{1, 2, 3, 4},
		1,
	)

	gotIDs := make([]int, 0, len(entries))
	for _, entry := range entries {
		gotIDs = append(gotIDs, entry.ID)
	}
	if !reflect.DeepEqual(gotIDs, []int{1, 2, 4, 3}) {
		t.Fatalf("unexpected franchise order: %+v", gotIDs)
	}

	if !entries[0].InUserList || entries[0].UserScore != 9 || entries[0].WatchedEpisodes != 12 {
		t.Fatalf("expected group member user state, got %+v", entries[0])
	}
	if entries[0].UserListStatus != string(AnimeListStatusCompleted) {
		t.Fatalf("expected completed user status, got %+v", entries[0])
	}
	if entries[0].RelationType != "" || entries[0].RelationTypeFormatted != "" {
		t.Fatalf("primary entry should not be decorated with relation metadata: %+v", entries[0])
	}
	if !entries[1].InUserList {
		t.Fatalf("expected owned group member, got %+v", entries[1])
	}
	if entries[1].RelationType != "sequel" || entries[1].RelationTypeFormatted != "Sequel" {
		t.Fatalf("owned group member should still be decorated with relation metadata, got %+v", entries[1])
	}
	if entries[2].RelationType != "spin_off" || entries[2].RelationTypeFormatted != "Spin off" {
		t.Fatalf("expected formatted fallback relation metadata, got %+v", entries[2])
	}
}

func TestAggregateFranchiseGenresUnionsDeduplicatesAndSorts(t *testing.T) {
	byAnime := map[int][]AnimeGenre{
		1: {{ID: 4, Name: "Comedy"}, {ID: 1, Name: "Action"}},
		2: {{ID: 1, Name: "Action"}, {ID: 2, Name: "Drama"}},
		// 3 is a member but has no genres yet (unresolved stub) -> contributes nothing.
		3: nil,
		// 99 is not a member, so its genre must not leak into the union.
		99: {{ID: 7, Name: "Horror"}},
	}

	got := AggregateFranchiseGenres(byAnime, []int{1, 2, 3})
	want := []AnimeGenre{
		{ID: 1, Name: "Action"},
		{ID: 4, Name: "Comedy"},
		{ID: 2, Name: "Drama"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AggregateFranchiseGenres = %+v, want %+v", got, want)
	}

	if AggregateFranchiseGenres(byAnime, nil) != nil {
		t.Fatal("AggregateFranchiseGenres with no members should be nil")
	}
	if AggregateFranchiseGenres(nil, []int{1}) != nil {
		t.Fatal("AggregateFranchiseGenres with no genre data should be nil")
	}
}

func TestEnsureAnimeDetailsGenresNormalizes(t *testing.T) {
	details := AnimeDetails{
		Genres: []AnimeGenre{
			{ID: 2, Name: "  Drama "},
			{ID: 1, Name: "Action"},
			{ID: 1, Name: "Action duplicate"},
			{ID: 0, Name: "Invalid id"},
			{ID: 3, Name: "   "},
		},
	}

	EnsureAnimeDetailsGenres(&details)

	want := []AnimeGenre{
		{ID: 1, Name: "Action"},
		{ID: 2, Name: "Drama"},
	}
	if !reflect.DeepEqual(details.Genres, want) {
		t.Fatalf("EnsureAnimeDetailsGenres = %+v, want %+v", details.Genres, want)
	}
}

func TestCloneAnimeDetailsCopiesGenres(t *testing.T) {
	original := AnimeDetails{Genres: []AnimeGenre{{ID: 1, Name: "Action"}}}
	cloned := CloneAnimeDetails(original)

	cloned.Genres[0].Name = "Mutated"
	if original.Genres[0].Name != "Action" {
		t.Fatalf("CloneAnimeDetails shared the genres slice: original mutated to %q", original.Genres[0].Name)
	}
}
