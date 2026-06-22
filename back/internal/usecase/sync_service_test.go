package usecase

import (
	"context"
	"testing"

	"test/internal/domain"
	"test/internal/ports"
)

type noopSyncLogger struct{}

func (noopSyncLogger) Debug(string, string, ...any) {}
func (noopSyncLogger) Info(string, string, ...any)  {}
func (noopSyncLogger) Warn(string, string, ...any)  {}
func (noopSyncLogger) Error(string, string, ...any) {}

type fakeSyncMALClient struct {
	ports.MALAnimeClient
	entries []domain.UserAnimeListEntry
	err     error
	calls   int
}

func (client *fakeSyncMALClient) FetchAnimeList(ctx context.Context, token string) ([]domain.UserAnimeListEntry, error) {
	client.calls++
	return client.entries, client.err
}

type fakeStubCatalogRepo struct {
	ports.AnimeCatalogRepository
	gotStubIDs []int
	calls      int
	err        error
}

func (repo *fakeStubCatalogRepo) UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error {
	repo.calls++
	repo.gotStubIDs = append([]int(nil), animeIDs...)
	return repo.err
}

type fakeUserAnimeSnapshotRepo struct {
	ports.UserAnimeRepository
	gotUserID    int64
	gotEntries   []domain.UserAnimeListEntry
	replaceCalls int
	clearCalls   int
	err          error
}

func (repo *fakeUserAnimeSnapshotRepo) ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []domain.UserAnimeListEntry) error {
	repo.replaceCalls++
	repo.gotUserID = userID
	repo.gotEntries = append([]domain.UserAnimeListEntry(nil), entries...)
	return repo.err
}

func (repo *fakeUserAnimeSnapshotRepo) ClearUserAnimeSnapshot(ctx context.Context, userID int64) error {
	repo.clearCalls++
	repo.gotUserID = userID
	return repo.err
}

type fakeSyncGuard struct {
	allow    bool
	begun    int
	finished int
}

func (guard *fakeSyncGuard) TryBeginUserSync(int64) bool { guard.begun++; return guard.allow }
func (guard *fakeSyncGuard) FinishUserSync(int64)        { guard.finished++ }

func TestSyncServiceLightPathUpsertsStubsAndReplacesItems(t *testing.T) {
	mal := &fakeSyncMALClient{entries: []domain.UserAnimeListEntry{
		{ID: 10, Title: "A", ListStatus: domain.AnimeListStatusWatching},
		{ID: 20, Title: "B", ListStatus: domain.AnimeListStatusCompleted},
	}}
	catalog := &fakeStubCatalogRepo{}
	userAnime := &fakeUserAnimeSnapshotRepo{}
	service := NewSyncService(SyncServiceDependencies{
		MAL:           mal,
		CatalogRepo:   catalog,
		UserAnimeRepo: userAnime,
		Guard:         &fakeSyncGuard{allow: true},
		Logger:        noopSyncLogger{},
	})

	if err := service.SyncAnimeWithProgressContext(context.Background(), 7, "tok", nil); err != nil {
		t.Fatalf("SyncAnimeWithProgressContext() returned error: %v", err)
	}

	if mal.calls != 1 {
		t.Fatalf("MAL FetchAnimeList calls = %d, want 1", mal.calls)
	}
	if catalog.calls != 1 || len(catalog.gotStubIDs) != 2 {
		t.Fatalf("UpsertAnimeCatalogStubs calls=%d ids=%v, want 1 call with 2 ids", catalog.calls, catalog.gotStubIDs)
	}
	if userAnime.replaceCalls != 1 || userAnime.gotUserID != 7 || len(userAnime.gotEntries) != 2 {
		t.Fatalf("ReplaceUserAnimeItems calls=%d userID=%d entries=%d, want 1/7/2", userAnime.replaceCalls, userAnime.gotUserID, len(userAnime.gotEntries))
	}
	if userAnime.clearCalls != 0 {
		t.Fatalf("ClearUserAnimeSnapshot calls = %d, want 0", userAnime.clearCalls)
	}
}

func TestSyncServiceLightPathClearsEmptySnapshot(t *testing.T) {
	mal := &fakeSyncMALClient{entries: nil}
	catalog := &fakeStubCatalogRepo{}
	userAnime := &fakeUserAnimeSnapshotRepo{}
	service := NewSyncService(SyncServiceDependencies{
		MAL:           mal,
		CatalogRepo:   catalog,
		UserAnimeRepo: userAnime,
		Guard:         &fakeSyncGuard{allow: true},
		Logger:        noopSyncLogger{},
	})

	if err := service.SyncAnimeWithProgressContext(context.Background(), 7, "tok", nil); err != nil {
		t.Fatalf("SyncAnimeWithProgressContext() returned error: %v", err)
	}

	if userAnime.clearCalls != 1 || userAnime.gotUserID != 7 {
		t.Fatalf("ClearUserAnimeSnapshot calls=%d userID=%d, want 1/7", userAnime.clearCalls, userAnime.gotUserID)
	}
	if catalog.calls != 0 || userAnime.replaceCalls != 0 {
		t.Fatalf("empty list must not upsert stubs (%d) or replace items (%d)", catalog.calls, userAnime.replaceCalls)
	}
}

func TestRunSyncWithJobSkipsWhenGuardBusy(t *testing.T) {
	mal := &fakeSyncMALClient{}
	service := NewSyncService(SyncServiceDependencies{
		MAL:           mal,
		CatalogRepo:   &fakeStubCatalogRepo{},
		UserAnimeRepo: &fakeUserAnimeSnapshotRepo{},
		Guard:         &fakeSyncGuard{allow: false},
		Logger:        noopSyncLogger{},
	})

	service.RunSyncWithJob(context.Background(), 7, "tok", nil)

	if mal.calls != 0 {
		t.Fatalf("a busy guard must skip the sync, but MAL was called %d times", mal.calls)
	}
}
