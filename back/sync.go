package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	animeDetailsPrimaryWorkers = 2
	animeDetailsRetryWorkers   = 2
	animeCatalogPersistBatch   = 25
	animeCatalogPersistWindow  = 15 * time.Millisecond
	franchiseHydrationWorkers  = 4
	maxNodesPerFranchise       = 40
)

func (a *App) runSync(userID int64, token string) {
	a.runSyncWithContext(context.Background(), userID, token)
}

func (a *App) runSyncWithContext(ctx context.Context, userID int64, token string) {
	a.runSyncWithJob(ctx, userID, token, nil)
}

func (a *App) runSyncWithJob(ctx context.Context, userID int64, token string, job *SyncJob) {
	ctx = ensureContext(ctx)

	if !a.tryBeginUserSync(userID) {
		err := errors.New("sync is already running for this user")
		job.Fail(err)
		a.logWarn("sync", "MAL sync skipped because another sync is already running", "user_id", userID)
		return
	}
	defer a.finishUserSync(userID)

	a.logInfo("sync", "MAL sync started", "user_id", userID)
	job.Start("Fetching completed MAL list")
	if err := a.syncAnimeWithProgressContext(ctx, userID, token, job); err != nil {
		job.Fail(err)
		a.logError("sync", "MAL sync failed", "user_id", userID, "err", err)
		return
	}
	job.Complete("Sync completed")
	a.logInfo("sync", "MAL sync completed", "user_id", userID)
}

func (a *App) runPublicSyncWithContext(ctx context.Context, userID int64, username string) {
	a.runPublicSyncWithJob(ctx, userID, username, nil)
}

func (a *App) runPublicSyncWithJob(ctx context.Context, userID int64, username string, job *SyncJob) {
	ctx = ensureContext(ctx)

	if !a.tryBeginUserSync(userID) {
		err := errors.New("sync is already running for this user")
		job.Fail(err)
		a.logWarn("sync", "public MAL sync skipped because another sync is already running", "username", username, "user_id", userID)
		return
	}
	defer a.finishUserSync(userID)

	a.logInfo("sync", "public MAL sync started", "username", username, "user_id", userID)
	job.Start("Fetching public MAL list")
	if err := a.syncPublicAnimeWithProgressContext(ctx, userID, username, job); err != nil {
		job.Fail(err)
		a.logError("sync", "public MAL sync failed", "username", username, "user_id", userID, "err", err)
		return
	}
	job.Complete("Public sync completed")
	a.logInfo("sync", "public MAL sync completed", "username", username, "user_id", userID)
}

func (a *App) SyncAnime(userID int64, token string) error {
	return a.syncAnimeWithContext(context.Background(), userID, token)
}

func (a *App) syncAnimeWithContext(ctx context.Context, userID int64, token string) error {
	return a.syncAnimeWithProgressContext(ctx, userID, token, nil)
}

func (a *App) syncAnimeWithProgressContext(ctx context.Context, userID int64, token string, job *SyncJob) error {
	ctx = ensureContext(ctx)

	job.Update(syncJobPhaseFetchingList, 0, 0, "Fetching completed MAL list")
	allEntries, err := a.FetchCompletedAnimeEntriesWithContext(ctx, token)
	if err != nil {
		return err
	}
	job.Update(syncJobPhaseListFetched, len(allEntries), len(allEntries), fmt.Sprintf("Fetched %d completed anime", len(allEntries)))

	return a.syncAnimeEntriesWithAuthContext(ctx, userID, allEntries, bearerMALAuth(token), job)
}

func (a *App) SyncPublicAnime(userID int64, username string) error {
	return a.syncPublicAnimeWithContext(context.Background(), userID, username)
}

func (a *App) syncPublicAnimeWithContext(ctx context.Context, userID int64, username string) error {
	return a.syncPublicAnimeWithProgressContext(ctx, userID, username, nil)
}

func (a *App) syncPublicAnimeWithProgressContext(ctx context.Context, userID int64, username string, job *SyncJob) error {
	ctx = ensureContext(ctx)

	job.Update(syncJobPhaseFetchingList, 0, 0, "Fetching public MAL list")
	allEntries, err := a.FetchPublicCompletedAnimeEntriesWithContext(ctx, username)
	if err != nil {
		return err
	}
	job.Update(syncJobPhaseListFetched, len(allEntries), len(allEntries), fmt.Sprintf("Fetched %d public completed anime", len(allEntries)))

	return a.syncAnimeEntriesWithAuthContext(ctx, userID, allEntries, clientIDMALAuth(a.Config.ClientID), job)
}

func (a *App) syncAnimeEntriesWithAuthContext(ctx context.Context, userID int64, allEntries []AnimeEntry, auth malAPIAuth, job *SyncJob) error {
	ctx = ensureContext(ctx)

	allEntries, duplicateCount := deduplicateAnimeEntriesPreserveOrder(allEntries)
	if duplicateCount > 0 {
		a.logWarn("sync", "dropped duplicate MAL completed entries before sync", "user_id", userID, "count", duplicateCount)
	}
	if len(allEntries) == 0 {
		job.Update(syncJobPhaseSavingSnapshot, 0, 0, "Clearing empty local snapshot")
		if err := a.clearUserAnimeSnapshotWithContext(ctx, userID); err != nil {
			return fmt.Errorf("cannot clear empty user snapshot: %w", err)
		}
		a.logInfo("sync", "no completed anime found, cleared user snapshot", "user_id", userID)
		return nil
	}

	cache, err := a.loadDetailsCache()
	if err != nil {
		a.logWarn("sync", "cannot load details cache", "err", err)
		cache = map[int]animeDetailsCacheItem{}
	}
	cacheStore := newAnimeDetailsCacheStore(a, cache, DetailsCacheFlushBatch)
	defer func() {
		if err := cacheStore.FlushPending(); err != nil {
			a.logWarn("sync", "cannot save details cache", "err", err)
		}
	}()

	entryIDs := uniqueAnimeIDs(allEntries)
	job.Update(syncJobPhaseSavingSnapshot, 0, len(entryIDs), "Saving local anime snapshot")
	if err := a.upsertAnimeCatalogStubsWithContext(ctx, entryIDs); err != nil {
		return fmt.Errorf("cannot upsert anime catalog stubs: %w", err)
	}

	job.Update(syncJobPhaseHydratingCatalog, 0, len(entryIDs), "Syncing anime details")
	if err := a.hydrateCatalogGraphWithAuthContext(ctx, auth, entryIDs, cacheStore, job); err != nil {
		return fmt.Errorf("cannot hydrate anime catalog graph: %w", err)
	}

	job.Update(syncJobPhaseGrouping, len(entryIDs), len(entryIDs), "Updating global anime franchises")
	if err := a.refreshAnimeFranchisesWithContext(ctx, entryIDs); err != nil {
		return fmt.Errorf("cannot refresh global anime franchises: %w", err)
	}

	job.Update(syncJobPhaseSavingSnapshot, len(entryIDs), len(entryIDs), "Saving local anime snapshot")
	if err := a.replaceUserAnimeItemsWithContext(ctx, userID, allEntries); err != nil {
		return fmt.Errorf("cannot save user anime items: %w", err)
	}

	job.Update(syncJobPhaseDone, len(entryIDs), len(entryIDs), "Finalizing sync")
	return nil
}

func (a *App) hydrateCatalogGraphWithContext(ctx context.Context, token string, seedIDs []int, cache *animeDetailsCacheStore) error {
	return a.hydrateCatalogGraphWithAuthContext(ctx, bearerMALAuth(token), seedIDs, cache, nil)
}

func (a *App) hydrateCatalogGraphWithAuthContext(ctx context.Context, auth malAPIAuth, seedIDs []int, cache *animeDetailsCacheStore, job *SyncJob) error {
	ctx = ensureContext(ctx)

	seedIDs = uniquePositiveIDs(seedIDs)
	if len(seedIDs) == 0 {
		return nil
	}

	resolver, err := newSyncCatalogResolverWithAuth(ctx, a, auth, cache)
	if err != nil {
		return err
	}
	defer resolver.Close()

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	seedQueue := make(chan int, len(seedIDs))
	workerCount := franchiseHydrationWorkers
	if workerCount > len(seedIDs) {
		workerCount = len(seedIDs)
	}

	var (
		firstErr       error
		errMu          sync.Mutex
		wg             sync.WaitGroup
		processedSeeds int64
	)
	setErr := func(err error) {
		if err == nil {
			return
		}

		errMu.Lock()
		defer errMu.Unlock()
		if firstErr != nil {
			return
		}

		firstErr = err
		cancel()
	}

	for workerID := 1; workerID <= workerCount; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-workerCtx.Done():
					return
				case seedID, ok := <-seedQueue:
					if !ok {
						return
					}
					if err := a.hydrateSingleFranchiseWithResolver(workerCtx, seedID, resolver); err != nil {
						if ctx.Err() != nil {
							setErr(ctx.Err())
							return
						}
						if workerCtx.Err() != nil {
							return
						}
						setErr(err)
						return
					}
					processed := int(atomic.AddInt64(&processedSeeds, 1))
					job.UpdateThrottled(
						syncJobPhaseHydratingCatalog,
						processed,
						len(seedIDs),
						"Syncing anime details",
						syncJobProgressUpdateInterval,
					)
				}
			}
		}()
	}

enqueueSeeds:
	for _, seedID := range seedIDs {
		select {
		case <-workerCtx.Done():
			break enqueueSeeds
		case seedQueue <- seedID:
		}
	}
	close(seedQueue)

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	job.Update(syncJobPhaseHydratingCatalog, len(seedIDs), len(seedIDs), "Anime details synced")
	return nil
}

func (a *App) hydrateSingleFranchiseWithContext(ctx context.Context, token string, seedID int, cache *animeDetailsCacheStore) error {
	ctx = ensureContext(ctx)

	resolver, err := newSyncCatalogResolver(ctx, a, token, cache)
	if err != nil {
		return err
	}
	defer resolver.Close()

	return a.hydrateSingleFranchiseWithResolver(ctx, seedID, resolver)
}

func (a *App) hydrateSingleFranchiseWithResolver(
	ctx context.Context,
	seedID int,
	resolver *syncCatalogResolver,
) error {
	ctx = ensureContext(ctx)

	queue := []int{seedID}
	visited := make(map[int]struct{}, maxNodesPerFranchise)

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		availableSlots := maxNodesPerFranchise - len(visited)
		if availableSlots <= 0 {
			a.logWarn("sync", "franchise traversal reached node cap", "seed_id", seedID, "cap", maxNodesPerFranchise)
			break
		}

		batch := make([]int, 0, availableSlots)
		queuedThisBatch := make(map[int]struct{}, availableSlots)
		for len(queue) > 0 && len(batch) < availableSlots {
			animeID := queue[0]
			queue = queue[1:]
			if animeID <= 0 {
				continue
			}
			if _, ok := visited[animeID]; ok {
				continue
			}
			if _, ok := queuedThisBatch[animeID]; ok {
				continue
			}

			visited[animeID] = struct{}{}
			queuedThisBatch[animeID] = struct{}{}
			batch = append(batch, animeID)
		}
		if len(batch) == 0 {
			continue
		}

		results, err := resolver.ResolveBatch(ctx, batch)
		if err != nil {
			return err
		}
		for _, result := range results {
			queue = append(queue, result.RelatedIDs...)
		}

		if len(visited) >= maxNodesPerFranchise && len(queue) > 0 {
			a.logWarn("sync", "franchise traversal reached node cap", "seed_id", seedID, "cap", maxNodesPerFranchise)
			break
		}
	}

	return nil
}

func (a *App) resolveAnimeCatalogBatchWithContext(
	ctx context.Context,
	token string,
	animeIDs []int,
	cache *animeDetailsCacheStore,
) ([]animeCatalogHydrationResult, error) {
	ctx = ensureContext(ctx)

	resolver, err := newSyncCatalogResolver(ctx, a, token, cache)
	if err != nil {
		return nil, err
	}
	defer resolver.Close()

	return resolver.ResolveBatch(ctx, animeIDs)
}

func (a *App) buildUserGroupsFromCatalogWithContext(ctx context.Context, allEntries []AnimeEntry) ([]GroupedView, []GroupedView, error) {
	ctx = ensureContext(ctx)

	allEntries, _ = deduplicateAnimeEntriesPreserveOrder(allEntries)

	ownedEntries := make([]AnimeEntry, 0, len(allEntries))
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
		componentIDs, truncated, err := a.collectFranchiseComponentWithContext(ctx, seed.ID)
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

		mediaType, err := a.getAnimeCatalogMediaTypeWithContext(ctx, animeID)
		if err != nil {
			return "", err
		}
		mediaTypeCache[animeID] = mediaType
		return mediaType, nil
	})
}

func buildUserGroupsFromObservedComponents(
	allEntries []AnimeEntry,
	observedMemberSets []map[int]struct{},
	mediaTypeLookup func(animeID int) (string, error),
) ([]GroupedView, []GroupedView, error) {
	allEntries, _ = deduplicateAnimeEntriesPreserveOrder(allEntries)

	ownedEntries := make([]AnimeEntry, 0, len(allEntries))
	idToIndex := make(map[int]int, len(allEntries))
	for _, entry := range allEntries {
		if entry.ID <= 0 {
			continue
		}
		if _, ok := idToIndex[entry.ID]; ok {
			continue
		}
		idToIndex[entry.ID] = len(ownedEntries)
		ownedEntries = append(ownedEntries, entry)
	}
	if len(ownedEntries) == 0 {
		return nil, nil, nil
	}

	parent := make([]int, len(ownedEntries))
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	for _, memberSet := range observedMemberSets {
		baseIndex := -1
		for memberID := range memberSet {
			memberIndex, ok := idToIndex[memberID]
			if !ok {
				continue
			}
			if baseIndex == -1 {
				baseIndex = memberIndex
				continue
			}
			union(baseIndex, memberIndex)
		}
	}

	type grouped struct {
		DisplayTitle       string
		NumEpisodesWatched int
		TotalScore         int
		ScoredItemsCount   int
		ItemsCount         int
		MemberIDs          map[int]struct{}
		Titles             map[string]struct{}
		HasMovie           bool
		HasNonMovie        bool
	}

	groups := make(map[int]*grouped)
	for i, entry := range ownedEntries {
		root := find(i)
		g := groups[root]
		if g == nil {
			g = &grouped{
				MemberIDs: make(map[int]struct{}),
				Titles:    make(map[string]struct{}),
			}
			groups[root] = g
		}
		if g.DisplayTitle == "" {
			g.DisplayTitle = entry.Title
		}

		g.NumEpisodesWatched += entry.NumEpisodesWatched
		if entry.Score > 0 {
			g.TotalScore += entry.Score
			g.ScoredItemsCount++
		}
		g.ItemsCount++
		g.Titles[entry.Title] = struct{}{}
		g.MemberIDs[entry.ID] = struct{}{}

		mediaType, err := mediaTypeLookup(entry.ID)
		if err != nil {
			return nil, nil, err
		}
		if mediaType == "movie" {
			g.HasMovie = true
		} else {
			g.HasNonMovie = true
		}
	}

	var seriesGroups []GroupedView
	var movieGroups []GroupedView
	for _, g := range groups {
		memberIDs, err := sortedMemberIDs(g.MemberIDs)
		if err != nil {
			return nil, nil, err
		}

		avgScore := 0.0
		if g.ScoredItemsCount > 0 {
			avgScore = math.Round((float64(g.TotalScore)/float64(g.ScoredItemsCount))*10) / 10
		}

		view := GroupedView{
			ID:                 memberIDs[0],
			GroupKey:           buildGroupKey(memberIDs),
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       len(g.Titles),
			AvgScore:           avgScore,
			GroupMemberIDs:     append([]int(nil), memberIDs...),
			WatchedEpisodesSum: g.NumEpisodesWatched,
		}

		if g.ItemsCount == 1 && g.HasMovie && !g.HasNonMovie {
			movieGroups = append(movieGroups, view)
			continue
		}
		seriesGroups = append(seriesGroups, view)
	}

	sortGroupedViews(seriesGroups)
	sortGroupedViews(movieGroups)
	return seriesGroups, movieGroups, nil
}

func (a *App) collectFranchiseComponentWithContext(ctx context.Context, seedID int) (map[int]struct{}, bool, error) {
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
		relatedIDs, err := a.listUndirectedAnimeRelationIDsWithContext(ctx, animeID)
		if err != nil {
			return nil, false, err
		}
		queue = append(queue, relatedIDs...)
	}

	return componentIDs, truncated, nil
}

func isAnimeCatalogStateFresh(state animeCatalogState, now time.Time) bool {
	if !state.Resolved || state.DetailsSyncedAt.IsZero() {
		return false
	}

	return now.Sub(state.DetailsSyncedAt) <= DetailsCacheTTL
}

func uniqueAnimeIDs(allEntries []AnimeEntry) []int {
	ids := make([]int, 0, len(allEntries))
	seen := make(map[int]struct{}, len(allEntries))
	for _, entry := range allEntries {
		if entry.ID <= 0 {
			continue
		}
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		seen[entry.ID] = struct{}{}
		ids = append(ids, entry.ID)
	}

	return ids
}

func deduplicateAnimeEntriesPreserveOrder(allEntries []AnimeEntry) ([]AnimeEntry, int) {
	deduplicated := make([]AnimeEntry, 0, len(allEntries))
	seen := make(map[int]struct{}, len(allEntries))
	duplicateCount := 0

	for _, entry := range allEntries {
		if entry.ID > 0 {
			if _, ok := seen[entry.ID]; ok {
				duplicateCount++
				continue
			}
			seen[entry.ID] = struct{}{}
		}

		deduplicated = append(deduplicated, entry)
	}

	return deduplicated, duplicateCount
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

type animeCatalogHydrationResult struct {
	AnimeID    int
	RelatedIDs []int
}

type animeCatalogResolvedNode struct {
	AnimeID    int
	RelatedIDs []int
}

type animeCatalogResolveTask struct {
	AnimeID         int
	Future          *animeCatalogResolveFuture
	SkipFreshLookup bool
}

type animeCatalogPersistTask struct {
	ResolveTask  animeCatalogResolveTask
	Details      AnimeDetailsInfo
	StoreInCache bool
}

type animeCatalogResolveFuture struct {
	done   chan struct{}
	result animeCatalogResolvedNode
	err    error
	once   sync.Once
}

type animeCatalogRetryError struct {
	AnimeID int
	Err     error
}

type syncCatalogResolver struct {
	app          *App
	auth         malAPIAuth
	cache        *animeDetailsCacheStore
	ctx          context.Context
	cancel       context.CancelFunc
	primaryQueue chan animeCatalogResolveTask
	persistQueue chan animeCatalogPersistTask
	retryQueue   chan animeCatalogResolveTask
	workerWG     sync.WaitGroup

	mu       sync.Mutex
	resolved map[int]animeCatalogResolvedNode
	inFlight map[int]*animeCatalogResolveFuture
}

func newSyncCatalogResolver(parentCtx context.Context, app *App, token string, cache *animeDetailsCacheStore) (*syncCatalogResolver, error) {
	return newSyncCatalogResolverWithAuth(parentCtx, app, bearerMALAuth(token), cache)
}

func newSyncCatalogResolverWithAuth(parentCtx context.Context, app *App, auth malAPIAuth, cache *animeDetailsCacheStore) (*syncCatalogResolver, error) {
	parentCtx = ensureContext(parentCtx)

	if cache == nil {
		return nil, fmt.Errorf("anime catalog resolver requires a cache store")
	}

	workerCtx, cancel := context.WithCancel(parentCtx)
	queueSize := franchiseHydrationWorkers * maxNodesPerFranchise
	if queueSize < animeDetailsPrimaryWorkers+animeDetailsRetryWorkers {
		queueSize = animeDetailsPrimaryWorkers + animeDetailsRetryWorkers
	}

	resolver := &syncCatalogResolver{
		app:          app,
		auth:         auth,
		cache:        cache,
		ctx:          workerCtx,
		cancel:       cancel,
		primaryQueue: make(chan animeCatalogResolveTask, queueSize),
		persistQueue: make(chan animeCatalogPersistTask, queueSize),
		retryQueue:   make(chan animeCatalogResolveTask, queueSize),
		resolved:     make(map[int]animeCatalogResolvedNode),
		inFlight:     make(map[int]*animeCatalogResolveFuture),
	}

	app.logDebug(
		"sync",
		"starting shared anime catalog resolver worker pools",
		"primary_workers", animeDetailsPrimaryWorkers,
		"retry_workers", animeDetailsRetryWorkers,
		"franchise_workers", franchiseHydrationWorkers,
	)

	for workerID := 1; workerID <= animeDetailsPrimaryWorkers; workerID++ {
		resolver.workerWG.Add(1)
		go resolver.runPrimaryWorker(workerID)
	}
	for workerID := 1; workerID <= animeDetailsRetryWorkers; workerID++ {
		resolver.workerWG.Add(1)
		go resolver.runRetryWorker(workerID)
	}
	resolver.workerWG.Add(1)
	go resolver.runPersistWorker()

	return resolver, nil
}

func (resolver *syncCatalogResolver) Close() {
	if resolver == nil {
		return
	}

	resolver.cancel()
	resolver.workerWG.Wait()
}

func (resolver *syncCatalogResolver) ResolveBatch(ctx context.Context, animeIDs []int) ([]animeCatalogHydrationResult, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return nil, nil
	}

	type pendingFuture struct {
		Index   int
		AnimeID int
		Future  *animeCatalogResolveFuture
	}

	results := make([]animeCatalogHydrationResult, len(animeIDs))
	pending := make([]pendingFuture, 0, len(animeIDs))
	newTasks := make([]animeCatalogResolveTask, 0, len(animeIDs))
	for index, animeID := range animeIDs {
		results[index].AnimeID = animeID

		node, future, shouldEnqueue := resolver.prepareResolve(animeID)
		if future == nil {
			results[index].RelatedIDs = append([]int(nil), node.RelatedIDs...)
			continue
		}

		pending = append(pending, pendingFuture{
			Index:   index,
			AnimeID: animeID,
			Future:  future,
		})
		if shouldEnqueue {
			newTasks = append(newTasks, animeCatalogResolveTask{AnimeID: animeID, Future: future})
		}
	}

	if err := resolver.preflightResolveTasks(ctx, newTasks); err != nil {
		return nil, err
	}

	var (
		firstErr    error
		retryErrors []string
	)
	for _, pendingFuture := range pending {
		node, err := pendingFuture.Future.await(ctx)
		if err != nil {
			var retryErr *animeCatalogRetryError
			switch {
			case errors.As(err, &retryErr):
				retryErrors = append(retryErrors, fmt.Sprintf("%d: %v", retryErr.AnimeID, retryErr.Err))
			case firstErr == nil:
				firstErr = err
			}
			continue
		}

		results[pendingFuture.Index].RelatedIDs = append([]int(nil), node.RelatedIDs...)
	}

	if firstErr != nil {
		return nil, firstErr
	}
	if len(retryErrors) > 0 {
		return nil, fmt.Errorf("failed to resolve anime details after retry for %d catalog ids: %s", len(retryErrors), summarizeRetryErrors(retryErrors))
	}

	return results, nil
}

func (resolver *syncCatalogResolver) preflightResolveTasks(ctx context.Context, tasks []animeCatalogResolveTask) error {
	if len(tasks) == 0 {
		return nil
	}

	animeIDs := make([]int, 0, len(tasks))
	for _, task := range tasks {
		animeIDs = append(animeIDs, task.AnimeID)
	}

	statesByID, err := resolver.app.getAnimeCatalogStatesByIDsWithContext(ctx, animeIDs)
	if err != nil {
		resolver.failResolveTasks(tasks, err)
		return err
	}

	now := time.Now()
	freshTasks := make([]animeCatalogResolveTask, 0, len(tasks))
	staleTasks := make([]animeCatalogResolveTask, 0, len(tasks))
	for _, task := range tasks {
		state, found := statesByID[task.AnimeID]
		if found && isAnimeCatalogStateFresh(state, now) {
			freshTasks = append(freshTasks, task)
			continue
		}

		task.SkipFreshLookup = true
		staleTasks = append(staleTasks, task)
	}

	relatedIDsBySource := map[int][]int{}
	if len(freshTasks) > 0 {
		freshIDs := make([]int, 0, len(freshTasks))
		for _, task := range freshTasks {
			freshIDs = append(freshIDs, task.AnimeID)
		}

		relatedIDsBySource, err = resolver.app.listAnimeRelationIDsBySourceIDsWithContext(ctx, freshIDs)
		if err != nil {
			resolver.failResolveTasks(tasks, err)
			return err
		}
	}

	for _, task := range freshTasks {
		resolver.finishTask(task, animeCatalogResolvedNode{
			AnimeID:    task.AnimeID,
			RelatedIDs: append([]int(nil), relatedIDsBySource[task.AnimeID]...),
		}, nil)
	}

	for index, task := range staleTasks {
		if err := resolver.enqueuePrimary(ctx, task); err != nil {
			resolver.failResolveTasks(staleTasks[index:], err)
			return err
		}
	}

	return nil
}

func (resolver *syncCatalogResolver) prepareResolve(animeID int) (animeCatalogResolvedNode, *animeCatalogResolveFuture, bool) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()

	if node, ok := resolver.resolved[animeID]; ok {
		return cloneAnimeCatalogResolvedNode(node), nil, false
	}
	if future, ok := resolver.inFlight[animeID]; ok {
		return animeCatalogResolvedNode{}, future, false
	}

	future := &animeCatalogResolveFuture{done: make(chan struct{})}
	resolver.inFlight[animeID] = future
	return animeCatalogResolvedNode{}, future, true
}

func (resolver *syncCatalogResolver) enqueuePrimary(ctx context.Context, task animeCatalogResolveTask) error {
	select {
	case <-ctx.Done():
		resolver.finishTask(task, animeCatalogResolvedNode{}, ctx.Err())
		return ctx.Err()
	case <-resolver.ctx.Done():
		err := resolver.ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		resolver.finishTask(task, animeCatalogResolvedNode{}, err)
		return err
	case resolver.primaryQueue <- task:
		return nil
	}
}

func (resolver *syncCatalogResolver) enqueuePersist(ctx context.Context, task animeCatalogPersistTask) error {
	select {
	case <-ctx.Done():
		resolver.finishTask(task.ResolveTask, animeCatalogResolvedNode{}, ctx.Err())
		return ctx.Err()
	case <-resolver.ctx.Done():
		err := resolver.ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		resolver.finishTask(task.ResolveTask, animeCatalogResolvedNode{}, err)
		return err
	case resolver.persistQueue <- task:
		return nil
	}
}

func (resolver *syncCatalogResolver) resolvePrimaryTask(ctx context.Context, task animeCatalogResolveTask) (animeCatalogResolvedNode, *animeCatalogPersistTask, bool, error) {
	if !task.SkipFreshLookup {
		state, found, err := resolver.app.getAnimeCatalogStateWithContext(ctx, task.AnimeID)
		if err != nil {
			return animeCatalogResolvedNode{}, nil, false, err
		}
		if found && isAnimeCatalogStateFresh(state, time.Now()) {
			relatedIDs, err := resolver.app.listAnimeRelationIDsWithContext(ctx, task.AnimeID)
			if err != nil {
				return animeCatalogResolvedNode{}, nil, false, err
			}
			return animeCatalogResolvedNode{
				AnimeID:    task.AnimeID,
				RelatedIDs: append([]int(nil), relatedIDs...),
			}, nil, false, nil
		}
	}

	details, err := resolver.app.fetchAnimeDetailsPrimaryWithAuthContext(ctx, resolver.auth, task.AnimeID, resolver.cache)
	if err != nil {
		return animeCatalogResolvedNode{}, nil, true, err
	}

	return animeCatalogResolvedNode{}, &animeCatalogPersistTask{
		ResolveTask:  task,
		Details:      details,
		StoreInCache: false,
	}, false, nil
}

func (resolver *syncCatalogResolver) resolveRetryTask(ctx context.Context, task animeCatalogResolveTask) (*animeCatalogPersistTask, error) {
	details, err := resolver.app.fetchAnimeDetailsRetryWithAuthContext(ctx, resolver.auth, task.AnimeID)
	if err != nil {
		return nil, err
	}

	return &animeCatalogPersistTask{
		ResolveTask:  task,
		Details:      details,
		StoreInCache: true,
	}, nil
}

func (resolver *syncCatalogResolver) persistResolvedBatch(ctx context.Context, tasks []animeCatalogPersistTask) {
	if len(tasks) == 0 {
		return
	}

	detailsBatch := make([]AnimeDetailsInfo, 0, len(tasks))
	for _, task := range tasks {
		details := cloneAnimeDetailsInfo(task.Details)
		if details.ID == 0 {
			details.ID = task.ResolveTask.AnimeID
		}
		ensureAnimeDetailsRelatedIDs(&details)
		if task.StoreInCache {
			if err := resolver.cache.StoreResolved(task.ResolveTask.AnimeID, details); err != nil {
				resolver.app.logWarn("cache", "cannot flush details cache batch", "id", task.ResolveTask.AnimeID, "err", err)
			}
		}
		detailsBatch = append(detailsBatch, details)
	}

	if err := resolver.app.saveAnimeCatalogDetailsBatchWithContext(ctx, detailsBatch); err != nil {
		for _, task := range tasks {
			resolver.finishTask(
				task.ResolveTask,
				animeCatalogResolvedNode{},
				fmt.Errorf("cannot save anime catalog details for id=%d: %w", task.ResolveTask.AnimeID, err),
			)
		}
		return
	}

	for index, task := range tasks {
		resolver.finishTask(task.ResolveTask, animeCatalogResolvedNode{
			AnimeID:    task.ResolveTask.AnimeID,
			RelatedIDs: collectTraversableRelatedIDs(detailsBatch[index]),
		}, nil)
	}
}

func (resolver *syncCatalogResolver) failResolveTasks(tasks []animeCatalogResolveTask, err error) {
	for _, task := range tasks {
		resolver.finishTask(task, animeCatalogResolvedNode{}, err)
	}
}

func (resolver *syncCatalogResolver) finishTask(task animeCatalogResolveTask, node animeCatalogResolvedNode, err error) {
	if err == nil {
		node = cloneAnimeCatalogResolvedNode(node)
	}

	resolver.mu.Lock()
	delete(resolver.inFlight, task.AnimeID)
	if err == nil {
		resolver.resolved[task.AnimeID] = node
	}
	resolver.mu.Unlock()

	task.Future.complete(node, err)
}

func cloneAnimeCatalogResolvedNode(node animeCatalogResolvedNode) animeCatalogResolvedNode {
	node.RelatedIDs = append([]int(nil), node.RelatedIDs...)
	return node
}

func (future *animeCatalogResolveFuture) complete(result animeCatalogResolvedNode, err error) {
	if future == nil {
		return
	}

	future.once.Do(func() {
		future.result = cloneAnimeCatalogResolvedNode(result)
		future.err = err
		close(future.done)
	})
}

func (future *animeCatalogResolveFuture) await(ctx context.Context) (animeCatalogResolvedNode, error) {
	if future == nil {
		return animeCatalogResolvedNode{}, nil
	}

	select {
	case <-ctx.Done():
		return animeCatalogResolvedNode{}, ctx.Err()
	case <-future.done:
		return cloneAnimeCatalogResolvedNode(future.result), future.err
	}
}

func (err *animeCatalogRetryError) Error() string {
	if err == nil || err.Err == nil {
		return ""
	}
	return err.Err.Error()
}

func (err *animeCatalogRetryError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func (resolver *syncCatalogResolver) runPrimaryWorker(workerID int) {
	defer resolver.workerWG.Done()

	for {
		select {
		case <-resolver.ctx.Done():
			return
		case task := <-resolver.primaryQueue:
			resolver.app.logDebug("sync", "resolving anime catalog details in primary worker", "worker", workerID, "id", task.AnimeID)
			node, persistTask, shouldRetry, err := resolver.resolvePrimaryTask(resolver.ctx, task)
			if err != nil {
				if resolver.ctx.Err() != nil {
					resolver.finishTask(task, animeCatalogResolvedNode{}, resolver.ctx.Err())
					return
				}
				if !shouldRetry {
					resolver.finishTask(task, animeCatalogResolvedNode{}, err)
					continue
				}

				resolver.app.logWarn("sync", "primary anime details lookup failed, queued for retry", "id", task.AnimeID, "err", err)
				select {
				case <-resolver.ctx.Done():
					resolver.finishTask(task, animeCatalogResolvedNode{}, resolver.ctx.Err())
					return
				case resolver.retryQueue <- task:
				}
				continue
			}

			if persistTask != nil {
				if err := resolver.enqueuePersist(resolver.ctx, *persistTask); err != nil && resolver.ctx.Err() == nil {
					resolver.finishTask(task, animeCatalogResolvedNode{}, err)
				}
				continue
			}

			resolver.finishTask(task, node, nil)
		}
	}
}

func (resolver *syncCatalogResolver) runRetryWorker(workerID int) {
	defer resolver.workerWG.Done()

	for {
		select {
		case <-resolver.ctx.Done():
			return
		case task := <-resolver.retryQueue:
			resolver.app.logDebug("sync", "retrying anime catalog details in retry worker", "worker", workerID, "id", task.AnimeID)
			persistTask, err := resolver.resolveRetryTask(resolver.ctx, task)
			if err != nil {
				if resolver.ctx.Err() != nil {
					resolver.finishTask(task, animeCatalogResolvedNode{}, resolver.ctx.Err())
					return
				}

				resolver.app.logWarn("sync", "background anime catalog details retry failed", "id", task.AnimeID, "err", err)
				resolver.finishTask(task, animeCatalogResolvedNode{}, &animeCatalogRetryError{AnimeID: task.AnimeID, Err: err})
				continue
			}

			if err := resolver.enqueuePersist(resolver.ctx, *persistTask); err != nil && resolver.ctx.Err() == nil {
				resolver.finishTask(task, animeCatalogResolvedNode{}, err)
			}
		}
	}
}

func (resolver *syncCatalogResolver) runPersistWorker() {
	defer resolver.workerWG.Done()

	for {
		var first animeCatalogPersistTask
		select {
		case <-resolver.ctx.Done():
			return
		case first = <-resolver.persistQueue:
		}

		batch := []animeCatalogPersistTask{first}
		timer := time.NewTimer(animeCatalogPersistWindow)

	collect:
		for len(batch) < animeCatalogPersistBatch {
			select {
			case <-resolver.ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				resolver.failPersistTasks(batch, resolver.ctx.Err())
				return
			case task := <-resolver.persistQueue:
				batch = append(batch, task)
			case <-timer.C:
				break collect
			}
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}

		resolver.persistResolvedBatch(resolver.ctx, batch)
	}
}

func (resolver *syncCatalogResolver) failPersistTasks(tasks []animeCatalogPersistTask, err error) {
	if err == nil {
		err = context.Canceled
	}
	for _, task := range tasks {
		resolver.finishTask(task.ResolveTask, animeCatalogResolvedNode{}, err)
	}
}

func summarizeRetryErrors(retryErrors []string) string {
	const maxShown = 3
	if len(retryErrors) <= maxShown {
		return strings.Join(retryErrors, "; ")
	}

	return strings.Join(retryErrors[:maxShown], "; ") + fmt.Sprintf("; and %d more", len(retryErrors)-maxShown)
}

func sortGroupedViews(groups []GroupedView) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].WatchedEpisodesSum == groups[j].WatchedEpisodesSum {
			return groups[i].DisplayTitle < groups[j].DisplayTitle
		}
		return groups[i].WatchedEpisodesSum > groups[j].WatchedEpisodesSum
	})
}

func sortedMemberIDs(memberIDs map[int]struct{}) ([]int, error) {
	if len(memberIDs) == 0 {
		return nil, fmt.Errorf("group has no MAL member ids")
	}

	ids := make([]int, 0, len(memberIDs))
	for id := range memberIDs {
		if id <= 0 {
			continue
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("group has no valid MAL member ids")
	}

	sort.Ints(ids)
	return ids, nil
}

func buildGroupKey(memberIDs []int) string {
	parts := make([]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ":")
}
