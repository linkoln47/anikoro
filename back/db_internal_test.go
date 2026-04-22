package main

import (
	"context"
	"reflect"
	"testing"
)

func TestDBInternal_NullableDate_NormalizesPartialMALDates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  any
	}{
		{name: "empty", input: "", want: nil},
		{name: "year", input: "2000", want: "2000-01-01"},
		{name: "year month", input: "2000-07", want: "2000-07-01"},
		{name: "full date", input: "2000-07-14", want: "2000-07-14"},
		{name: "invalid", input: "unknown", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullableDate(tt.input)
			if got != tt.want {
				t.Fatalf("nullableDate(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDBInternal_EnsureAnimeDetailsRelatedIDs_DeduplicatesRelatedEntries(t *testing.T) {
	details := AnimeDetailsInfo{
		Related: []AnimeRelationInfo{
			{ID: 20, Title: "Original title"},
			{ID: 20, RelationType: "sequel", RelationTypeFormatted: "Sequel"},
			{ID: 30, Title: "Movie follow-up", RelationType: "side_story", RelationTypeFormatted: "Side story"},
		},
		RelatedIDs: []int{20, 30, 20, 40, 0, -5},
	}

	ensureAnimeDetailsRelatedIDs(&details)

	wantRelated := []AnimeRelationInfo{
		{ID: 20, Title: "Original title", RelationType: "sequel", RelationTypeFormatted: "Sequel"},
		{ID: 30, Title: "Movie follow-up", RelationType: "side_story", RelationTypeFormatted: "Side story"},
		{ID: 40},
	}
	if !reflect.DeepEqual(details.Related, wantRelated) {
		t.Fatalf("details.Related = %#v, want %#v", details.Related, wantRelated)
	}

	wantRelatedIDs := []int{20, 30, 40}
	if !reflect.DeepEqual(details.RelatedIDs, wantRelatedIDs) {
		t.Fatalf("details.RelatedIDs = %#v, want %#v", details.RelatedIDs, wantRelatedIDs)
	}
}

func TestDBInternal_CollectTraversableRelatedIDs_IgnoresCharacterAndOther(t *testing.T) {
	details := AnimeDetailsInfo{
		Related: []AnimeRelationInfo{
			{ID: 10, RelationType: "sequel", RelationTypeFormatted: "Sequel"},
			{ID: 20, RelationType: "other", RelationTypeFormatted: "Other"},
			{ID: 30, RelationType: "character", RelationTypeFormatted: "Character"},
			{ID: 40},
		},
		RelatedIDs: []int{10, 20, 30, 40, 50},
	}

	got := collectTraversableRelatedIDs(details)
	want := []int{10, 40, 50}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectTraversableRelatedIDs() = %#v, want %#v", got, want)
	}
}

func TestDBInternal_SaveAnimeCatalogDetailsBatchWithContext_DeduplicatesDuplicateRelations(t *testing.T) {
	app, mock := newInternalTestApp(t)

	detailsBatch := []AnimeDetailsInfo{{
		ID:        4023,
		Title:     "Kitty's Paradise",
		MediaType: "tv",
		Related: []AnimeRelationInfo{
			{ID: 22443, Title: "Hello Kitty no Alps no Shoujo Heidi", RelationType: "other", RelationTypeFormatted: "Other"},
			{ID: 22443, Title: "Hello Kitty no Alps no Shoujo Heidi"},
			{ID: 60485, Title: "Hello Kitty to Asobou! Manabou!", RelationType: "other", RelationTypeFormatted: "Other"},
		},
		RelatedIDs: []int{22443, 60485, 22443},
	}}

	expectSaveAnimeCatalogDetailsBatch(t, mock, detailsBatch)

	if err := app.saveAnimeCatalogDetailsBatchWithContext(context.Background(), detailsBatch); err != nil {
		t.Fatalf("saveAnimeCatalogDetailsBatchWithContext returned error: %v", err)
	}
}
