package app

import (
	"context"
	"time"

	"test/internal/domain"
	"test/internal/usecase"
)

const (
	animeCatalogPersistWindow = 15 * time.Millisecond
	maxNodesPerFranchise      = usecase.MaxNodesPerFranchise
)

func (a *App) syncCatalogHydrator() *syncCatalogHydrator {
	logger := appSyncLogger{app: a}
	return newSyncCatalogHydrator(a.MALAnimeClient, newPostgresCatalogRepository(a.DB), logger)
}

func (a *App) hydrateCatalogGraphWithContext(ctx context.Context, token string, seedIDs []int, cache AnimeDetailsCacheStore) error {
	return a.hydrateCatalogGraphWithAuthContext(ctx, bearerMALAuth(token), seedIDs, cache, nil)
}

func (a *App) hydrateCatalogGraphWithAuthContext(ctx context.Context, auth MALAuth, seedIDs []int, cache AnimeDetailsCacheStore, job SyncProgressReporter) error {
	return a.syncCatalogHydrator().HydrateCatalogGraph(ctx, auth, seedIDs, cache, job)
}

func (a *App) hydrateSingleFranchiseWithContext(ctx context.Context, token string, seedID int, cache AnimeDetailsCacheStore) error {
	return a.syncCatalogHydrator().HydrateSingleFranchiseWithToken(ctx, token, seedID, cache)
}

func (a *App) resolveAnimeCatalogBatchWithContext(
	ctx context.Context,
	token string,
	animeIDs []int,
	cache AnimeDetailsCacheStore,
) ([]animeCatalogHydrationResult, error) {
	return a.syncCatalogHydrator().ResolveAnimeCatalogBatchWithToken(ctx, token, animeIDs, cache)
}

func (a *App) buildUserGroupsFromCatalogWithContext(ctx context.Context, allEntries []CompletedAnimeEntry) ([]GroupedView, []GroupedView, error) {
	ctx = ensureContext(ctx)
	catalogRepo := newPostgresCatalogRepository(a.DB)

	allEntries, _ = domain.DeduplicateCompletedAnimeEntriesPreserveOrder(allEntries)

	ownedEntries := make([]CompletedAnimeEntry, 0, len(allEntries))
	ownedEntryIDs := make(map[int]struct{}, len(allEntries))
	for _, entry := range allEntries {
		if entry.ID <= 0 {
			continue
		}
		if _, ok := ownedEntryIDs[entry.ID]; ok {
			continue
		}
		ownedEntries = append(ownedEntries, entry)
		ownedEntryIDs[entry.ID] = struct{}{}
	}
	if len(ownedEntries) == 0 {
		return nil, nil, nil
	}

	observedMemberSets := make([]map[int]struct{}, 0, len(ownedEntries))
	for _, seed := range ownedEntries {
		componentIDs, truncated, err := a.collectFranchiseComponentWithContext(ctx, catalogRepo, seed.ID)
		if err != nil {
			return nil, nil, err
		}
		if truncated {
			a.logWarn("sync", "group construction reached node cap", "seed_id", seed.ID, "cap", maxNodesPerFranchise)
		}

		memberSet := make(map[int]struct{})
		for memberID := range componentIDs {
			if _, ok := ownedEntryIDs[memberID]; ok {
				memberSet[memberID] = struct{}{}
			}
		}
		if len(memberSet) == 0 {
			memberSet[seed.ID] = struct{}{}
		}

		observedMemberSets = append(observedMemberSets, memberSet)
	}

	mediaTypeCache := make(map[int]string, len(ownedEntries))
	return buildUserGroupsFromObservedComponents(allEntries, observedMemberSets, func(animeID int) (string, error) {
		if mediaType, ok := mediaTypeCache[animeID]; ok {
			return mediaType, nil
		}

		mediaType, err := catalogRepo.GetAnimeCatalogMediaType(ctx, animeID)
		if err != nil {
			return "", err
		}
		mediaTypeCache[animeID] = mediaType
		return mediaType, nil
	})
}

func buildUserGroupsFromObservedComponents(
	allEntries []CompletedAnimeEntry,
	observedMemberSets []map[int]struct{},
	mediaTypeLookup func(animeID int) (string, error),
) ([]GroupedView, []GroupedView, error) {
	return usecase.BuildUserGroupsFromObservedComponents(allEntries, observedMemberSets, mediaTypeLookup)
}

func summarizeRetryErrors(retryErrors []string) string {
	return usecase.SummarizeRetryErrors(retryErrors)
}

func (a *App) collectFranchiseComponentWithContext(ctx context.Context, catalogRepo AnimeCatalogRepository, seedID int) (map[int]struct{}, bool, error) {
	ctx = ensureContext(ctx)

	componentIDs := make(map[int]struct{}, maxNodesPerFranchise)
	queue := []int{seedID}
	truncated := false

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}

		animeID := queue[0]
		queue = queue[1:]
		if animeID <= 0 {
			continue
		}
		if _, ok := componentIDs[animeID]; ok {
			continue
		}
		if len(componentIDs) >= maxNodesPerFranchise {
			truncated = true
			break
		}

		componentIDs[animeID] = struct{}{}
		relatedIDs, err := catalogRepo.ListUndirectedAnimeRelationIDs(ctx, animeID)
		if err != nil {
			return nil, false, err
		}
		queue = append(queue, relatedIDs...)
	}

	return componentIDs, truncated, nil
}

func uniquePositiveIDs(ids []int) []int {
	unique := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return unique
}
