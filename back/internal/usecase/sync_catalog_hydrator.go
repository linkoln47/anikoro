package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	animeDetailsPrimaryWorkers = 2
	animeDetailsRetryWorkers   = 2
	animeCatalogPersistBatch   = 25
	animeCatalogPersistWindow  = 15 * time.Millisecond
	franchiseHydrationWorkers  = 4
	MaxNodesPerFranchise       = 40
	maxNodesPerFranchise       = MaxNodesPerFranchise
)

type SyncCatalogHydrator struct {
	mal         ports.MALAnimeClient
	catalogRepo ports.AnimeCatalogRepository
	logger      ports.SyncLogger
}

func NewSyncCatalogHydrator(mal ports.MALAnimeClient, catalogRepo ports.AnimeCatalogRepository, logger ports.SyncLogger) *SyncCatalogHydrator {
	return &SyncCatalogHydrator{
		mal:         mal,
		catalogRepo: catalogRepo,
		logger:      logger,
	}
}

func (hydrator *SyncCatalogHydrator) debug(component, msg string, args ...any) {
	if hydrator != nil && hydrator.logger != nil {
		hydrator.logger.Debug(component, msg, args...)
	}
}

func (hydrator *SyncCatalogHydrator) warn(component, msg string, args ...any) {
	if hydrator != nil && hydrator.logger != nil {
		hydrator.logger.Warn(component, msg, args...)
	}
}

func (hydrator *SyncCatalogHydrator) HydrateCatalogGraph(ctx context.Context, auth ports.MALAuth, seedIDs []int, cache ports.AnimeDetailsCacheStore, job ports.SyncProgressReporter) error {
	ctx = ensureContext(ctx)
	job = ensureSyncProgressReporter(job)

	seedIDs = uniquePositiveIDs(seedIDs)
	if len(seedIDs) == 0 {
		return nil
	}

	resolver, err := newSyncCatalogResolverWithAuth(ctx, hydrator.mal, hydrator.catalogRepo, hydrator.logger, auth, cache)
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
					if err := hydrator.hydrateSingleFranchiseWithResolver(workerCtx, seedID, resolver); err != nil {
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
						SyncJobPhaseHydratingCatalog,
						processed,
						len(seedIDs),
						"Syncing anime details",
						SyncJobProgressUpdateInterval,
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

	job.Update(SyncJobPhaseHydratingCatalog, len(seedIDs), len(seedIDs), "Anime details synced")
	return nil
}

func (hydrator *SyncCatalogHydrator) HydrateSingleFranchiseWithToken(ctx context.Context, token string, seedID int, cache ports.AnimeDetailsCacheStore) error {
	ctx = ensureContext(ctx)

	resolver, err := newSyncCatalogResolver(ctx, hydrator.mal, hydrator.catalogRepo, hydrator.logger, token, cache)
	if err != nil {
		return err
	}
	defer resolver.Close()

	return hydrator.hydrateSingleFranchiseWithResolver(ctx, seedID, resolver)
}

func (hydrator *SyncCatalogHydrator) hydrateSingleFranchiseWithResolver(
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
			hydrator.warn("sync", "franchise traversal reached node cap", "seed_id", seedID, "cap", maxNodesPerFranchise)
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
			hydrator.warn("sync", "franchise traversal reached node cap", "seed_id", seedID, "cap", maxNodesPerFranchise)
			break
		}
	}

	return nil
}

func (hydrator *SyncCatalogHydrator) ResolveAnimeCatalogBatchWithToken(
	ctx context.Context,
	token string,
	animeIDs []int,
	cache ports.AnimeDetailsCacheStore,
) ([]AnimeCatalogHydrationResult, error) {
	ctx = ensureContext(ctx)

	resolver, err := newSyncCatalogResolver(ctx, hydrator.mal, hydrator.catalogRepo, hydrator.logger, token, cache)
	if err != nil {
		return nil, err
	}
	defer resolver.Close()

	return resolver.ResolveBatch(ctx, animeIDs)
}

func BuildUserGroupsFromObservedComponents(
	allEntries []domain.CompletedAnimeEntry,
	observedMemberSets []map[int]struct{},
	mediaTypeLookup func(animeID int) (string, error),
) ([]domain.GroupedView, []domain.GroupedView, error) {
	allEntries, _ = domain.DeduplicateCompletedAnimeEntriesPreserveOrder(allEntries)

	ownedEntries := make([]domain.CompletedAnimeEntry, 0, len(allEntries))
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

	var seriesGroups []domain.GroupedView
	var movieGroups []domain.GroupedView
	for _, g := range groups {
		memberIDs, err := domain.SortedMemberIDs(g.MemberIDs)
		if err != nil {
			return nil, nil, err
		}

		avgScore := 0.0
		if g.ScoredItemsCount > 0 {
			avgScore = domain.RoundScore(float64(g.TotalScore) / float64(g.ScoredItemsCount))
		}

		view := domain.GroupedView{
			ID:                 memberIDs[0],
			GroupKey:           domain.BuildGroupKey(memberIDs),
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

	domain.SortGroupedViews(seriesGroups)
	domain.SortGroupedViews(movieGroups)
	return seriesGroups, movieGroups, nil
}

func isAnimeCatalogStateFresh(state domain.AnimeCatalogState, now time.Time) bool {
	if !state.Resolved || state.DetailsSyncedAt.IsZero() {
		return false
	}

	return now.Sub(state.DetailsSyncedAt) <= DetailsCacheTTL
}

type AnimeCatalogHydrationResult struct {
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
	Details      domain.AnimeDetails
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
	mal          ports.MALAnimeClient
	catalogRepo  ports.AnimeCatalogRepository
	logger       ports.SyncLogger
	auth         ports.MALAuth
	cache        ports.AnimeDetailsCacheStore
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

func newSyncCatalogResolver(parentCtx context.Context, mal ports.MALAnimeClient, catalogRepo ports.AnimeCatalogRepository, logger ports.SyncLogger, token string, cache ports.AnimeDetailsCacheStore) (*syncCatalogResolver, error) {
	return newSyncCatalogResolverWithAuth(parentCtx, mal, catalogRepo, logger, bearerMALAuth(token), cache)
}

func newSyncCatalogResolverWithAuth(parentCtx context.Context, mal ports.MALAnimeClient, catalogRepo ports.AnimeCatalogRepository, logger ports.SyncLogger, auth ports.MALAuth, cache ports.AnimeDetailsCacheStore) (*syncCatalogResolver, error) {
	parentCtx = ensureContext(parentCtx)

	if cache == nil {
		return nil, fmt.Errorf("anime catalog resolver requires a cache store")
	}
	if mal == nil {
		return nil, fmt.Errorf("anime catalog resolver requires a MAL client")
	}
	if catalogRepo == nil {
		return nil, fmt.Errorf("anime catalog resolver requires a catalog repository")
	}

	workerCtx, cancel := context.WithCancel(parentCtx)
	queueSize := franchiseHydrationWorkers * maxNodesPerFranchise
	if queueSize < animeDetailsPrimaryWorkers+animeDetailsRetryWorkers {
		queueSize = animeDetailsPrimaryWorkers + animeDetailsRetryWorkers
	}

	resolver := &syncCatalogResolver{
		mal:          mal,
		catalogRepo:  catalogRepo,
		logger:       logger,
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

	resolver.debug("sync", "starting shared anime catalog resolver worker pools", "primary_workers", animeDetailsPrimaryWorkers, "retry_workers", animeDetailsRetryWorkers, "franchise_workers", franchiseHydrationWorkers)

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

func (resolver *syncCatalogResolver) debug(component, msg string, args ...any) {
	if resolver != nil && resolver.logger != nil {
		resolver.logger.Debug(component, msg, args...)
	}
}

func (resolver *syncCatalogResolver) warn(component, msg string, args ...any) {
	if resolver != nil && resolver.logger != nil {
		resolver.logger.Warn(component, msg, args...)
	}
}

func (resolver *syncCatalogResolver) ResolveBatch(ctx context.Context, animeIDs []int) ([]AnimeCatalogHydrationResult, error) {
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

	results := make([]AnimeCatalogHydrationResult, len(animeIDs))
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
		return nil, fmt.Errorf("failed to resolve anime details after retry for %d catalog ids: %s", len(retryErrors), SummarizeRetryErrors(retryErrors))
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

	statesByID, err := resolver.catalogRepo.GetAnimeCatalogStates(ctx, animeIDs)
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

		relatedIDsBySource, err = resolver.catalogRepo.ListAnimeRelationIDsBySourceIDs(ctx, freshIDs)
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
		state, found, err := resolver.catalogRepo.GetAnimeCatalogState(ctx, task.AnimeID)
		if err != nil {
			return animeCatalogResolvedNode{}, nil, false, err
		}
		if found && isAnimeCatalogStateFresh(state, time.Now()) {
			relatedIDs, err := resolver.catalogRepo.ListAnimeRelationIDs(ctx, task.AnimeID)
			if err != nil {
				return animeCatalogResolvedNode{}, nil, false, err
			}
			return animeCatalogResolvedNode{
				AnimeID:    task.AnimeID,
				RelatedIDs: append([]int(nil), relatedIDs...),
			}, nil, false, nil
		}
	}

	details, err := resolver.mal.FetchAnimeDetails(ctx, resolver.auth, task.AnimeID, resolver.cache, ports.AnimeDetailsFetchPrimary)
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
	details, err := resolver.mal.FetchAnimeDetails(ctx, resolver.auth, task.AnimeID, resolver.cache, ports.AnimeDetailsFetchRetry)
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

	detailsBatch := make([]domain.AnimeDetails, 0, len(tasks))
	for _, task := range tasks {
		details := domain.CloneAnimeDetails(task.Details)
		if details.ID == 0 {
			details.ID = task.ResolveTask.AnimeID
		}
		domain.EnsureAnimeDetailsRelatedIDs(&details)
		if task.StoreInCache {
			if err := resolver.cache.StoreResolved(task.ResolveTask.AnimeID, details); err != nil {
				resolver.warn("cache", "cannot flush details cache batch", "id", task.ResolveTask.AnimeID, "err", err)
			}
		}
		detailsBatch = append(detailsBatch, details)
	}

	if err := resolver.catalogRepo.SaveAnimeCatalogDetailsBatch(ctx, detailsBatch); err != nil {
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
			RelatedIDs: domain.CollectTraversableRelatedIDs(detailsBatch[index]),
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
			resolver.debug("sync", "resolving anime catalog details in primary worker", "worker", workerID, "id", task.AnimeID)
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

				resolver.warn("sync", "primary anime details lookup failed, queued for retry", "id", task.AnimeID, "err", err)
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
			resolver.debug("sync", "retrying anime catalog details in retry worker", "worker", workerID, "id", task.AnimeID)
			persistTask, err := resolver.resolveRetryTask(resolver.ctx, task)
			if err != nil {
				if resolver.ctx.Err() != nil {
					resolver.finishTask(task, animeCatalogResolvedNode{}, resolver.ctx.Err())
					return
				}

				resolver.warn("sync", "background anime catalog details retry failed", "id", task.AnimeID, "err", err)
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

func SummarizeRetryErrors(retryErrors []string) string {
	const maxShown = 3
	if len(retryErrors) <= maxShown {
		return strings.Join(retryErrors, "; ")
	}

	return strings.Join(retryErrors[:maxShown], "; ") + fmt.Sprintf("; and %d more", len(retryErrors)-maxShown)
}
