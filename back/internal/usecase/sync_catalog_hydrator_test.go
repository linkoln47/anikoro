package usecase

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

type fakeHydrationMALClient struct {
	ports.MALAnimeClient
	mu    sync.Mutex
	calls map[int]int
}

func (client *fakeHydrationMALClient) FetchPublicAnimeDetails(_ context.Context, animeID int, _ ports.AnimeDetailsFetchMode) (domain.AnimeDetails, error) {
	client.mu.Lock()
	client.calls[animeID]++
	client.mu.Unlock()

	if animeID == 2268 {
		return domain.AnimeDetails{}, &ports.AnimeDetailsFetchError{
			AnimeID:    animeID,
			StatusCode: http.StatusNotFound,
			Kind:       ports.AnimeDetailsFetchErrorNotFound,
			Retryable:  false,
		}
	}
	return domain.AnimeDetails{ID: animeID, Title: "Available", MediaType: "tv"}, nil
}

func (client *fakeHydrationMALClient) callCount(animeID int) int {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.calls[animeID]
}

type fakeResolverCatalogRepo struct {
	ports.AnimeCatalogRepository
	mu     sync.Mutex
	states map[int]domain.AnimeCatalogState
}

func (repo *fakeResolverCatalogRepo) GetAnimeCatalogStates(_ context.Context, animeIDs []int) (map[int]domain.AnimeCatalogState, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()

	states := make(map[int]domain.AnimeCatalogState, len(animeIDs))
	for _, animeID := range animeIDs {
		if state, ok := repo.states[animeID]; ok {
			states[animeID] = state
		}
	}
	return states, nil
}

func (repo *fakeResolverCatalogRepo) GetAnimeCatalogState(_ context.Context, animeID int) (domain.AnimeCatalogState, bool, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	state, ok := repo.states[animeID]
	return state, ok, nil
}

func (repo *fakeResolverCatalogRepo) ListAnimeRelationIDsBySourceIDs(_ context.Context, _ []int) (map[int][]int, error) {
	return map[int][]int{}, nil
}

func (repo *fakeResolverCatalogRepo) SaveAnimeCatalogDetailsBatch(_ context.Context, detailsBatch []domain.AnimeDetails) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, details := range detailsBatch {
		repo.states[details.ID] = domain.AnimeCatalogState{
			Resolved:        true,
			DetailsSyncedAt: time.Now(),
		}
	}
	return nil
}

func TestHydratePublicCatalogGraphDefersNotFoundWithoutFailingBatch(t *testing.T) {
	malClient := &fakeHydrationMALClient{calls: map[int]int{}}
	catalogRepo := &fakeResolverCatalogRepo{states: map[int]domain.AnimeCatalogState{
		5:    {Resolved: false},
		2268: {Resolved: false},
	}}
	failures := &fakeHydrationFailureStore{deferred: map[int]bool{}}
	hydrator := NewSyncCatalogHydratorWithFailureStore(malClient, catalogRepo, failures, noopSyncLogger{})

	err := hydrator.HydratePublicCatalogGraph(
		context.Background(),
		[]int{5, 2268},
		fakeCacheStore{},
		nil,
	)
	if err != nil {
		t.Fatalf("HydratePublicCatalogGraph() returned error: %v", err)
	}

	states, err := catalogRepo.GetAnimeCatalogStates(context.Background(), []int{5, 2268})
	if err != nil {
		t.Fatalf("GetAnimeCatalogStates() returned error: %v", err)
	}
	if !states[5].Resolved {
		t.Fatal("available anime was not persisted")
	}
	if states[2268].Resolved {
		t.Fatal("404 anime must remain unresolved")
	}
	if failures.ShouldAttempt(2268, time.Now()) {
		t.Fatal("404 anime was not deferred")
	}
	if malClient.callCount(2268) != 1 {
		t.Fatalf("404 lookups = %d, want 1 without immediate retry", malClient.callCount(2268))
	}
}
