package usecase

import (
	"context"
	"errors"
	"testing"

	"test/internal/domain"
	"test/internal/ports"
)

type fakeMALListWriter struct {
	state      domain.AnimeUserListState
	err        error
	gotAnimeID int
	gotToken   string
	gotPatch   domain.UserAnimeListPatch
	calls      int
}

func (writer *fakeMALListWriter) UpdateAnimeListStatus(ctx context.Context, token string, animeID int, patch domain.UserAnimeListPatch) (domain.AnimeUserListState, error) {
	writer.calls++
	writer.gotToken = token
	writer.gotAnimeID = animeID
	writer.gotPatch = patch
	return writer.state, writer.err
}

func (writer *fakeMALListWriter) DeleteAnimeListStatus(ctx context.Context, token string, animeID int) error {
	writer.calls++
	writer.gotToken = token
	writer.gotAnimeID = animeID
	return writer.err
}

type fakeCatalogSummaryRepo struct {
	ports.AnimeCatalogRepository
	summary domain.AnimeCatalogSummary
	found   bool
	err     error
}

func (repo *fakeCatalogSummaryRepo) GetAnimeCatalogSummary(ctx context.Context, animeID int) (domain.AnimeCatalogSummary, bool, error) {
	return repo.summary, repo.found, repo.err
}

type fakeUserAnimeWriter struct {
	ports.UserAnimeRepository
	err       error
	gotUserID int64
	gotEntry  domain.UserAnimeListEntry
	calls     int
}

func (repo *fakeUserAnimeWriter) UpsertUserAnimeItem(ctx context.Context, userID int64, entry domain.UserAnimeListEntry) error {
	repo.calls++
	repo.gotUserID = userID
	repo.gotEntry = entry
	return repo.err
}

func newListEditServiceForTest(writer *fakeMALListWriter, catalog *fakeCatalogSummaryRepo, userAnime *fakeUserAnimeWriter) *ListEditService {
	return NewListEditService(ListEditServiceDependencies{
		MALWriter:     writer,
		CatalogRepo:   catalog,
		UserAnimeRepo: userAnime,
	})
}

func TestUpdateUserAnimeListEntrySuccess(t *testing.T) {
	writer := &fakeMALListWriter{
		state: domain.AnimeUserListState{
			Score:           8,
			WatchedEpisodes: 12,
			ListStatus:      "completed",
		},
	}
	catalog := &fakeCatalogSummaryRepo{
		summary: domain.AnimeCatalogSummary{AnimeID: 42, Title: "Test Anime", NumEpisodes: 12},
		found:   true,
	}
	userAnime := &fakeUserAnimeWriter{}
	service := newListEditServiceForTest(writer, catalog, userAnime)

	episodes := 12
	updated, err := service.UpdateUserAnimeListEntry(context.Background(), 7, "token-1", 42, domain.UserAnimeListPatch{
		WatchedEpisodes: &episodes,
	})
	if err != nil {
		t.Fatalf("UpdateUserAnimeListEntry() returned error: %v", err)
	}

	if writer.calls != 1 || writer.gotAnimeID != 42 || writer.gotToken != "token-1" {
		t.Fatalf("MAL writer call mismatch: calls=%d animeID=%d token=%q", writer.calls, writer.gotAnimeID, writer.gotToken)
	}
	if userAnime.calls != 1 || userAnime.gotUserID != 7 {
		t.Fatalf("local upsert call mismatch: calls=%d userID=%d", userAnime.calls, userAnime.gotUserID)
	}
	if userAnime.gotEntry.ListStatus != domain.AnimeListStatusCompleted ||
		userAnime.gotEntry.Score != 8 ||
		userAnime.gotEntry.NumEpisodesWatched != 12 ||
		userAnime.gotEntry.Title != "Test Anime" {
		t.Fatalf("local upsert entry mismatch: %+v", userAnime.gotEntry)
	}
	if updated.ListStatus != domain.AnimeListStatusCompleted || updated.NumEpisodes != 12 || updated.Title != "Test Anime" {
		t.Fatalf("updated entry mismatch: %+v", updated)
	}
}

func TestUpdateUserAnimeListEntryRejectsUnknownAnime(t *testing.T) {
	writer := &fakeMALListWriter{}
	catalog := &fakeCatalogSummaryRepo{found: false}
	userAnime := &fakeUserAnimeWriter{}
	service := newListEditServiceForTest(writer, catalog, userAnime)

	score := 5
	_, err := service.UpdateUserAnimeListEntry(context.Background(), 7, "token", 42, domain.UserAnimeListPatch{Score: &score})
	if !errors.Is(err, ErrAnimeNotInCatalog) {
		t.Fatalf("error = %v, want ErrAnimeNotInCatalog", err)
	}
	if writer.calls != 0 {
		t.Fatalf("MAL writer must not be called for unknown anime")
	}
}

func TestUpdateUserAnimeListEntryRejectsEpisodesAboveTotal(t *testing.T) {
	writer := &fakeMALListWriter{}
	catalog := &fakeCatalogSummaryRepo{
		summary: domain.AnimeCatalogSummary{AnimeID: 42, Title: "Test Anime", NumEpisodes: 12},
		found:   true,
	}
	userAnime := &fakeUserAnimeWriter{}
	service := newListEditServiceForTest(writer, catalog, userAnime)

	episodes := 13
	_, err := service.UpdateUserAnimeListEntry(context.Background(), 7, "token", 42, domain.UserAnimeListPatch{WatchedEpisodes: &episodes})
	if !errors.Is(err, ErrInvalidListEditInput) {
		t.Fatalf("error = %v, want ErrInvalidListEditInput", err)
	}
	if !errors.Is(err, domain.ErrWatchedEpisodesExceedTotal) {
		t.Fatalf("error = %v, want wrapped ErrWatchedEpisodesExceedTotal", err)
	}
	if writer.calls != 0 {
		t.Fatalf("MAL writer must not be called for invalid patch")
	}
}

func TestUpdateUserAnimeListEntryDoesNotSaveOnMALFailure(t *testing.T) {
	writer := &fakeMALListWriter{err: errors.New("mal is down")}
	catalog := &fakeCatalogSummaryRepo{
		summary: domain.AnimeCatalogSummary{AnimeID: 42, Title: "Test Anime", NumEpisodes: 12},
		found:   true,
	}
	userAnime := &fakeUserAnimeWriter{}
	service := newListEditServiceForTest(writer, catalog, userAnime)

	score := 5
	_, err := service.UpdateUserAnimeListEntry(context.Background(), 7, "token", 42, domain.UserAnimeListPatch{Score: &score})
	if !errors.Is(err, ErrMALListUpdateFailed) {
		t.Fatalf("error = %v, want ErrMALListUpdateFailed", err)
	}
	if userAnime.calls != 0 {
		t.Fatalf("local snapshot must not change when the MAL update fails")
	}
}

func TestUpdateUserAnimeListEntryReportsSaveFailure(t *testing.T) {
	writer := &fakeMALListWriter{
		state: domain.AnimeUserListState{Score: 5, WatchedEpisodes: 1, ListStatus: "watching"},
	}
	catalog := &fakeCatalogSummaryRepo{
		summary: domain.AnimeCatalogSummary{AnimeID: 42, NumEpisodes: 12},
		found:   true,
	}
	userAnime := &fakeUserAnimeWriter{err: errors.New("db is down")}
	service := newListEditServiceForTest(writer, catalog, userAnime)

	score := 5
	_, err := service.UpdateUserAnimeListEntry(context.Background(), 7, "token", 42, domain.UserAnimeListPatch{Score: &score})
	if !errors.Is(err, ErrListEntrySaveFailed) {
		t.Fatalf("error = %v, want ErrListEntrySaveFailed", err)
	}
}
