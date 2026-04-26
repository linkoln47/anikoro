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

	movie := entries[1]
	if movie.Item.ID != 9 || movie.Item.Type != AnimeListItemTypeMovie {
		t.Fatalf("unexpected standalone movie entry: %+v", movie.Item)
	}

	stats := CountAnimeListStats(entries)
	if stats.SeriesCount != 1 || stats.MoviesCount != 1 || stats.TotalCount != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
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
			1: {Score: 9, WatchedEpisodes: 12},
			2: {Score: 8, WatchedEpisodes: 10},
		},
		map[int]map[int]AnimeRelation{
			1: {
				2: {ID: 2, RelationType: "sequel"},
			},
			2: {
				4: {ID: 4, RelationType: "spin_off"},
			},
		},
		[]int{1},
		[]int{1, 2, 3, 4},
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
	if entries[0].RelationType != "" || entries[0].RelationTypeFormatted != "" {
		t.Fatalf("group member should not be decorated with relation metadata: %+v", entries[0])
	}
	if entries[1].RelationType != "sequel" || entries[1].RelationTypeFormatted != "Sequel" {
		t.Fatalf("expected formatted direct relation metadata, got %+v", entries[1])
	}
	if entries[2].RelationType != "spin_off" || entries[2].RelationTypeFormatted != "Spin off" {
		t.Fatalf("expected formatted fallback relation metadata, got %+v", entries[2])
	}
}
