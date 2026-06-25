package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

type fakeStubLister struct {
	ids   []int
	limit int
	calls int
	err   error
}

func (lister *fakeStubLister) ListUnresolvedCatalogIDs(ctx context.Context, limit int) ([]int, error) {
	lister.calls++
	lister.limit = limit
	return lister.ids, lister.err
}

type fakePublicHydrator struct {
	ports.AnimeCatalogHydrator
	gotSeedIDs  []int
	publicCalls int
	err         error
}

func (hydrator *fakePublicHydrator) HydratePublicCatalogGraph(ctx context.Context, seedIDs []int, cache ports.AnimeDetailsCacheStore, reporter ports.SyncProgressReporter) error {
	hydrator.publicCalls++
	hydrator.gotSeedIDs = append([]int(nil), seedIDs...)
	return hydrator.err
}

type fakeFranchiseRefresher struct {
	gotSeedIDs []int
	calls      int
	err        error
}

func (repo *fakeFranchiseRefresher) RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error {
	repo.calls++
	repo.gotSeedIDs = append([]int(nil), seedIDs...)
	return repo.err
}

type fakeDetailsCache struct {
	store ports.AnimeDetailsCacheStore
}

func (cache *fakeDetailsCache) OpenDetailsCache(ctx context.Context) (ports.AnimeDetailsCacheStore, error) {
	return cache.store, nil
}

type fakeCacheStore struct{}

func (fakeCacheStore) StoreResolved(int, domain.AnimeDetails) error { return nil }
func (fakeCacheStore) StagedDetails() []ports.CachedAnimeDetails    { return nil }
func (fakeCacheStore) MarkPersisted([]int) error                    { return nil }
func (fakeCacheStore) FlushPending() error                          { return nil }

type fakeCatalogStateRepo struct {
	ports.AnimeCatalogRepository
	states map[int]domain.AnimeCatalogState
}

func (repo *fakeCatalogStateRepo) GetAnimeCatalogStates(context.Context, []int) (map[int]domain.AnimeCatalogState, error) {
	return repo.states, nil
}

type fakeHydrationFailureStore struct {
	mu       sync.Mutex
	deferred map[int]bool
}

func (store *fakeHydrationFailureStore) ShouldAttempt(animeID int, _ time.Time) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	return !store.deferred[animeID]
}

func (store *fakeHydrationFailureStore) DeferredCount(time.Time) int {
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for _, deferred := range store.deferred {
		if deferred {
			count++
		}
	}
	return count
}

func (store *fakeHydrationFailureStore) RecordNotFound(animeID int, _ time.Time) (time.Time, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.deferred[animeID] = true
	return time.Now().Add(time.Hour), nil
}

func (store *fakeHydrationFailureStore) MarkSucceeded(animeIDs []int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, animeID := range animeIDs {
		delete(store.deferred, animeID)
	}
	return nil
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLazyHydrationResolveStubs(t *testing.T) {
	stubs := &fakeStubLister{ids: []int{5, 9}}
	hydrator := &fakePublicHydrator{}
	franchise := &fakeFranchiseRefresher{}
	service := NewLazyHydrationService(LazyHydrationServiceDependencies{
		Stubs:         stubs,
		Hydrator:      hydrator,
		FranchiseRepo: franchise,
		CatalogRepo: &fakeCatalogStateRepo{states: map[int]domain.AnimeCatalogState{
			5: {Resolved: true},
			9: {Resolved: true},
		}},
		Cache:  &fakeDetailsCache{store: fakeCacheStore{}},
		Logger: noopSyncLogger{},
	})

	processed, err := service.ResolveStubs(context.Background(), 50)
	if err != nil {
		t.Fatalf("ResolveStubs() returned error: %v", err)
	}

	if processed != 2 {
		t.Fatalf("processed = %d, want 2", processed)
	}
	if stubs.calls != 1 || stubs.limit != 50 {
		t.Fatalf("ListUnresolvedCatalogIDs calls=%d limit=%d, want 1/50", stubs.calls, stubs.limit)
	}
	if hydrator.publicCalls != 1 || !equalIntSlices(hydrator.gotSeedIDs, []int{5, 9}) {
		t.Fatalf("HydratePublicCatalogGraph calls=%d ids=%v, want 1 with [5 9]", hydrator.publicCalls, hydrator.gotSeedIDs)
	}
	if franchise.calls != 0 {
		t.Fatalf("ResolveStubs must leave franchise persistence to the hydration sink (RefreshAnimeFranchises calls=%d)", franchise.calls)
	}
}

func TestLazyHydrationResolveStubsSkipsDeferredAndCountsOnlyResolved(t *testing.T) {
	stubs := &fakeStubLister{ids: []int{1, 5, 9}}
	hydrator := &fakePublicHydrator{}
	franchise := &fakeFranchiseRefresher{}
	failures := &fakeHydrationFailureStore{deferred: map[int]bool{1: true}}
	service := NewLazyHydrationService(LazyHydrationServiceDependencies{
		Stubs:         stubs,
		Hydrator:      hydrator,
		FranchiseRepo: franchise,
		CatalogRepo: &fakeCatalogStateRepo{states: map[int]domain.AnimeCatalogState{
			5: {Resolved: true},
			9: {Resolved: false},
		}},
		Cache:    &fakeDetailsCache{store: fakeCacheStore{}},
		Failures: failures,
		Logger:   noopSyncLogger{},
	})

	processed, err := service.ResolveStubs(context.Background(), 2)
	if err != nil {
		t.Fatalf("ResolveStubs() returned error: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if stubs.limit != 3 {
		t.Fatalf("ListUnresolvedCatalogIDs limit = %d, want 3", stubs.limit)
	}
	if !equalIntSlices(hydrator.gotSeedIDs, []int{5, 9}) {
		t.Fatalf("hydrated ids = %v, want [5 9]", hydrator.gotSeedIDs)
	}
	if franchise.calls != 0 {
		t.Fatalf("ResolveStubs must not run the legacy franchise refresh path (calls=%d ids=%v)", franchise.calls, franchise.gotSeedIDs)
	}
}

func TestLazyHydrationResolveStubsEmptyQueue(t *testing.T) {
	stubs := &fakeStubLister{ids: nil}
	hydrator := &fakePublicHydrator{}
	franchise := &fakeFranchiseRefresher{}
	service := NewLazyHydrationService(LazyHydrationServiceDependencies{
		Stubs:         stubs,
		Hydrator:      hydrator,
		FranchiseRepo: franchise,
		Cache:         &fakeDetailsCache{store: fakeCacheStore{}},
		Logger:        noopSyncLogger{},
	})

	processed, err := service.ResolveStubs(context.Background(), 50)
	if err != nil {
		t.Fatalf("ResolveStubs() returned error: %v", err)
	}
	if processed != 0 {
		t.Fatalf("processed = %d, want 0", processed)
	}
	if hydrator.publicCalls != 0 || franchise.calls != 0 {
		t.Fatalf("empty queue must not hydrate (%d) or refresh franchises (%d)", hydrator.publicCalls, franchise.calls)
	}
}

func TestLazyHydrationResolveStubsSkipsFranchiseOnHydrateError(t *testing.T) {
	stubs := &fakeStubLister{ids: []int{1}}
	hydrator := &fakePublicHydrator{err: errors.New("mal is down")}
	franchise := &fakeFranchiseRefresher{}
	service := NewLazyHydrationService(LazyHydrationServiceDependencies{
		Stubs:         stubs,
		Hydrator:      hydrator,
		FranchiseRepo: franchise,
		Cache:         &fakeDetailsCache{store: fakeCacheStore{}},
		Logger:        noopSyncLogger{},
	})

	if _, err := service.ResolveStubs(context.Background(), 50); err == nil {
		t.Fatalf("ResolveStubs() must surface the hydrate error")
	}
	if franchise.calls != 0 {
		t.Fatalf("franchises must not be refreshed when hydration fails (calls=%d)", franchise.calls)
	}
}
