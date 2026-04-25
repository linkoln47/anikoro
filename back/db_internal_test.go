package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"test/internal/domain"
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
	details := AnimeDetails{
		Related: []AnimeRelation{
			{ID: 20, Title: "Original title"},
			{ID: 20, RelationType: "sequel", RelationTypeFormatted: "Sequel"},
			{ID: 30, Title: "Movie follow-up", RelationType: "side_story", RelationTypeFormatted: "Side story"},
		},
		RelatedIDs: []int{20, 30, 20, 40, 0, -5},
	}

	domain.EnsureAnimeDetailsRelatedIDs(&details)

	wantRelated := []AnimeRelation{
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
	details := AnimeDetails{
		Related: []AnimeRelation{
			{ID: 10, RelationType: "sequel", RelationTypeFormatted: "Sequel"},
			{ID: 20, RelationType: "other", RelationTypeFormatted: "Other"},
			{ID: 30, RelationType: "character", RelationTypeFormatted: "Character"},
			{ID: 40},
		},
		RelatedIDs: []int{10, 20, 30, 40, 50},
	}

	got := domain.CollectTraversableRelatedIDs(details)
	want := []int{10, 40, 50}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectTraversableRelatedIDs() = %#v, want %#v", got, want)
	}
}

func TestDBInternal_SaveAnimeCatalogDetailsBatchWithContext_DeduplicatesDuplicateRelations(t *testing.T) {
	app, mock := newInternalTestApp(t)

	detailsBatch := []AnimeDetails{{
		ID:        4023,
		Title:     "Kitty's Paradise",
		MediaType: "tv",
		Related: []AnimeRelation{
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

func TestDBInternal_ReplaceUserAnimeItemsWithContext_RewritesOnlyCurrentUserRows(t *testing.T) {
	app, mock := newInternalTestApp(t)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT set_config\\('app.user_id', \\$1, true\\)").
		WithArgs("42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM user_anime_items WHERE user_id = \\$1").
		WithArgs(testUserID).
		WillReturnResult(sqlmock.NewResult(0, 2))

	prepare := mock.ExpectPrepare("INSERT INTO user_anime_items")
	prepare.ExpectExec().
		WithArgs(testUserID, 10, "Series A", 9, 12, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	prepare.ExpectExec().
		WithArgs(testUserID, 20, "Movie B", 7, 1, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := newPostgresSyncAnimeRepository(app.DB, appSyncLogger{app: app}).ReplaceUserAnimeItems(context.Background(), testUserID, []CompletedAnimeEntry{
		{ID: 10, Title: "Series A", Score: 9, NumEpisodesWatched: 12},
		{ID: 20, Title: "Movie B", Score: 7, NumEpisodesWatched: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceUserAnimeItems returned error: %v", err)
	}
}

func TestDBInternal_RefreshAnimeFranchisesWithContext_StoresGlobalComponent(t *testing.T) {
	app, mock := newInternalTestApp(t)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT DISTINCT franchise_id\\s+FROM anime_franchise_members\\s+WHERE anime_id IN").
		WithArgs(10).
		WillReturnRows(internalSQLRows("franchise_id"))
	expectUndirectedAnimeRelationIDs(mock, 10, 20)
	expectUndirectedAnimeRelationIDs(mock, 20, 10, 30)
	expectUndirectedAnimeRelationIDs(mock, 30, 20)
	mock.ExpectQuery("SELECT DISTINCT franchise_id\\s+FROM anime_franchise_members\\s+WHERE anime_id IN").
		WithArgs(10, 20, 30).
		WillReturnRows(internalSQLRows("franchise_id"))
	mock.ExpectQuery("INSERT INTO anime_franchises").
		WithArgs("10:20:30").
		WillReturnRows(internalSQLRows("id").AddRow(int64(500)))
	mock.ExpectExec("INSERT INTO anime_franchise_members").
		WithArgs(10, int64(500), 20, int64(500), 30, int64(500)).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectExec("DELETE FROM anime_franchises f").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	if err := newPostgresSyncAnimeRepository(app.DB, appSyncLogger{app: app}).RefreshAnimeFranchises(context.Background(), []int{10}); err != nil {
		t.Fatalf("RefreshAnimeFranchises returned error: %v", err)
	}
}
