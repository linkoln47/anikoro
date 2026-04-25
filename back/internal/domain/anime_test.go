package domain

import (
	"reflect"
	"testing"
)

func TestDeduplicateCompletedAnimeEntriesPreserveOrder(t *testing.T) {
	entries := []CompletedAnimeEntry{
		{ID: 2, Title: "two"},
		{ID: 1, Title: "one"},
		{ID: 2, Title: "two duplicate"},
		{ID: 0, Title: "invalid ids are kept"},
		{ID: 0, Title: "invalid ids are not deduplicated"},
	}

	got, duplicates := DeduplicateCompletedAnimeEntriesPreserveOrder(entries)
	if duplicates != 1 {
		t.Fatalf("duplicate count = %d, want 1", duplicates)
	}

	want := []CompletedAnimeEntry{
		{ID: 2, Title: "two"},
		{ID: 1, Title: "one"},
		{ID: 0, Title: "invalid ids are kept"},
		{ID: 0, Title: "invalid ids are not deduplicated"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deduplicated entries = %#v, want %#v", got, want)
	}
}

func TestRoundScore(t *testing.T) {
	if got := RoundScore(8.666); got != 8.7 {
		t.Fatalf("RoundScore() = %v, want 8.7", got)
	}
}

func TestBuildGroupKey(t *testing.T) {
	if got := BuildGroupKey([]int{2, 10, 30}); got != "2:10:30" {
		t.Fatalf("BuildGroupKey() = %q, want %q", got, "2:10:30")
	}
}

func TestSortGroupedViews(t *testing.T) {
	groups := []GroupedView{
		{DisplayTitle: "Beta", WatchedEpisodesSum: 12},
		{DisplayTitle: "Alpha", WatchedEpisodesSum: 12},
		{DisplayTitle: "Gamma", WatchedEpisodesSum: 24},
	}

	SortGroupedViews(groups)

	got := []string{groups[0].DisplayTitle, groups[1].DisplayTitle, groups[2].DisplayTitle}
	want := []string{"Gamma", "Alpha", "Beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted titles = %#v, want %#v", got, want)
	}
}

func TestCollectTraversableRelatedIDs(t *testing.T) {
	details := AnimeDetails{
		Related: []AnimeRelation{
			{ID: 2, RelationType: "sequel"},
			{ID: 3, RelationType: "character"},
			{ID: 4, RelationType: "other"},
		},
		RelatedIDs: []int{5, 2},
	}

	got := CollectTraversableRelatedIDs(details)
	want := []int{2, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("traversable related ids = %#v, want %#v", got, want)
	}
}

func TestEnsureAnimeDetailsRelatedIDsMergesMetadata(t *testing.T) {
	details := AnimeDetails{
		Related: []AnimeRelation{
			{ID: 10, Title: "Known", RelationType: "sequel"},
			{ID: 10, RelationTypeFormatted: "Sequel"},
		},
		RelatedIDs: []int{10, 20},
	}

	EnsureAnimeDetailsRelatedIDs(&details)

	wantRelated := []AnimeRelation{
		{ID: 10, Title: "Known", RelationType: "sequel", RelationTypeFormatted: "Sequel"},
		{ID: 20},
	}
	if !reflect.DeepEqual(details.Related, wantRelated) {
		t.Fatalf("related = %#v, want %#v", details.Related, wantRelated)
	}
	if !reflect.DeepEqual(details.RelatedIDs, []int{10, 20}) {
		t.Fatalf("related ids = %#v, want %#v", details.RelatedIDs, []int{10, 20})
	}
}
