package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	animeDetailsPrimaryWorkers = 2
	animeDetailsRetryWorkers   = 2
)

func (a *App) runSync(userID int64, token string) {
	a.runSyncWithContext(context.Background(), userID, token)
}

func (a *App) runSyncWithContext(ctx context.Context, userID int64, token string) {
	ctx = ensureContext(ctx)

	if !a.tryBeginUserSync(userID) {
		a.logWarn("sync", "MAL sync skipped because another sync is already running", "user_id", userID)
		return
	}
	defer a.finishUserSync(userID)

	a.logInfo("sync", "MAL sync started", "user_id", userID)
	if err := a.syncAnimeWithContext(ctx, userID, token); err != nil {
		a.logError("sync", "MAL sync failed", "user_id", userID, "err", err)
		return
	}
	a.logInfo("sync", "MAL sync completed", "user_id", userID)
}

func (a *App) syncAnime(userID int64, token string) error {
	return a.syncAnimeWithContext(context.Background(), userID, token)
}

func (a *App) syncAnimeWithContext(ctx context.Context, userID int64, token string) error {
	ctx = ensureContext(ctx)

	allEntries, err := a.fetchCompletedAnimeEntriesWithContext(ctx, token)
	if err != nil {
		return err
	}
	if len(allEntries) == 0 {
		a.logInfo("sync", "no completed anime found")
		return nil
	}

	cache, err := a.loadDetailsCache()
	if err != nil {
		a.logWarn("sync", "cannot load details cache", "err", err)
		cache = map[int]animeDetailsCacheItem{}
	}
	cacheStore := newAnimeDetailsCacheStore(a, cache, detailsCacheFlushBatch)
	defer func() {
		if err := cacheStore.FlushPending(); err != nil {
			a.logWarn("sync", "cannot save details cache", "err", err)
		}
	}()

	seriesGroups, movieGroups, err := a.groupCompletedAnimeEntriesWithContext(ctx, token, allEntries, cacheStore)
	if err != nil {
		return err
	}

	if err := a.saveGroupedListsWithContext(ctx, userID, seriesGroups, movieGroups); err != nil {
		return fmt.Errorf("cannot save grouped anime entries to database: %w", err)
	}

	return nil
}

type primaryAnimeDetailsResolver func(token string, animeID int, cache *animeDetailsCacheStore) (animeDetailsInfo, error)
type retryAnimeDetailsResolver func(token string, animeID int) (animeDetailsInfo, error)

type animeDetailsTask struct {
	Entry animeEntry
	Index int
}

type animeDetailsResult struct {
	Details animeDetailsInfo
	Entry   animeEntry
	Err     error
	Index   int
}

func (a *App) groupCompletedAnimeEntries(token string, allEntries []animeEntry, cache *animeDetailsCacheStore) ([]groupedView, []groupedView, error) {
	return a.groupCompletedAnimeEntriesWithContext(context.Background(), token, allEntries, cache)
}

func (a *App) groupCompletedAnimeEntriesWithContext(ctx context.Context, token string, allEntries []animeEntry, cache *animeDetailsCacheStore) ([]groupedView, []groupedView, error) {
	ctx = ensureContext(ctx)

	return a.groupCompletedAnimeEntriesWithResolversAndContext(ctx, token, allEntries, cache, nil, nil)
}

func (a *App) groupCompletedAnimeEntriesWithResolvers(
	token string,
	allEntries []animeEntry,
	cache *animeDetailsCacheStore,
	primaryResolver primaryAnimeDetailsResolver,
	retryResolver retryAnimeDetailsResolver,
) ([]groupedView, []groupedView, error) {
	return a.groupCompletedAnimeEntriesWithResolversAndContext(context.Background(), token, allEntries, cache, primaryResolver, retryResolver)
}

func (a *App) groupCompletedAnimeEntriesWithResolversAndContext(
	ctx context.Context,
	token string,
	allEntries []animeEntry,
	cache *animeDetailsCacheStore,
	primaryResolver primaryAnimeDetailsResolver,
	retryResolver retryAnimeDetailsResolver,
) ([]groupedView, []groupedView, error) {
	ctx = ensureContext(ctx)

	parent := make([]int, len(allEntries))
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

	idToIndexes := make(map[int][]int)
	for i, entry := range allEntries {
		if entry.ID != 0 {
			idToIndexes[entry.ID] = append(idToIndexes[entry.ID], i)
		}
	}

	primaryResolverWithContext := func(token string, animeID int, cache *animeDetailsCacheStore) (animeDetailsInfo, error) {
		return a.fetchAnimeDetailsPrimaryWithContext(ctx, token, animeID, cache)
	}
	retryResolverWithContext := func(token string, animeID int) (animeDetailsInfo, error) {
		return a.fetchAnimeDetailsRetryWithContext(ctx, token, animeID)
	}
	if primaryResolver != nil {
		primaryResolverWithContext = primaryResolver
	}
	if retryResolver != nil {
		retryResolverWithContext = retryResolver
	}

	uniqueTasks := uniqueAnimeDetailsTasks(allEntries)
	a.logDebug(
		"sync",
		"starting anime details worker pools",
		"primary_workers", animeDetailsPrimaryWorkers,
		"retry_workers", animeDetailsRetryWorkers,
		"unique_ids", len(uniqueTasks),
	)

	primaryQueue := make(chan animeDetailsTask, len(uniqueTasks))
	primaryResults := make(chan animeDetailsResult, len(uniqueTasks))
	retryQueue := make(chan animeDetailsTask, len(uniqueTasks))
	retryResults := make(chan animeDetailsResult, len(uniqueTasks))

	var primaryWG sync.WaitGroup
	for workerID := 1; workerID <= animeDetailsPrimaryWorkers; workerID++ {
		primaryWG.Add(1)
		go a.runAnimeDetailsPrimaryWorker(ctx, workerID, token, cache, primaryQueue, primaryResults, primaryResolverWithContext, &primaryWG)
	}
	go func() {
		primaryWG.Wait()
		close(primaryResults)
	}()

	var retryWG sync.WaitGroup
	for workerID := 1; workerID <= animeDetailsRetryWorkers; workerID++ {
		retryWG.Add(1)
		go a.runAnimeDetailsRetryWorker(ctx, workerID, token, retryQueue, retryResults, retryResolverWithContext, &retryWG)
	}
	go func() {
		retryWG.Wait()
		close(retryResults)
	}()

enqueuePrimary:
	for _, task := range uniqueTasks {
		select {
		case <-ctx.Done():
			break enqueuePrimary
		case primaryQueue <- task:
		}
	}
	close(primaryQueue)

	detailsMap := make(map[int]animeDetailsInfo)
	for result := range primaryResults {
		if result.Err != nil {
			if ctx.Err() != nil {
				continue
			}
			a.logWarn("sync", "primary anime details lookup failed, queued for retry", "id", result.Entry.ID, "err", result.Err)
			select {
			case <-ctx.Done():
			case retryQueue <- animeDetailsTask{Entry: result.Entry, Index: result.Index}:
			}
			continue
		}

		detailsMap[result.Entry.ID] = result.Details
		applyRelatedAnimeLinks(result.Index, result.Details, idToIndexes, union)
	}

	close(retryQueue)

	var retryErrors []string
	for result := range retryResults {
		if result.Err != nil {
			if ctx.Err() != nil {
				continue
			}
			retryErrors = append(retryErrors, fmt.Sprintf("%d (%s): %v", result.Entry.ID, result.Entry.Title, result.Err))
			continue
		}

		detailsMap[result.Entry.ID] = result.Details
		if err := cache.StoreResolved(result.Entry.ID, result.Details); err != nil {
			a.logWarn("cache", "cannot flush details cache batch", "id", result.Entry.ID, "err", err)
		}
		applyRelatedAnimeLinks(result.Index, result.Details, idToIndexes, union)
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if len(retryErrors) > 0 {
		return nil, nil, fmt.Errorf("failed to resolve anime details after retry for %d entries: %s", len(retryErrors), summarizeRetryErrors(retryErrors))
	}

	type grouped struct {
		ID                 int
		GroupKey           string
		DisplayTitle       string
		NumEpisodesWatched int
		TotalScore         int
		ItemsCount         int
		MemberIDs          map[int]struct{}
		Titles             map[string]struct{}
		HasMovie           bool
		HasNonMovie        bool
		IsIsolatedMovie    bool
	}

	groups := make(map[int]*grouped)
	for i, entry := range allEntries {
		root := find(i)
		g := groups[root]
		if g == nil {
			g = &grouped{
				DisplayTitle: entry.Title,
				MemberIDs:    make(map[int]struct{}),
				Titles:       make(map[string]struct{}),
			}
			groups[root] = g
		}

		g.NumEpisodesWatched += entry.NumEpisodesWatched
		g.TotalScore += entry.Score
		g.ItemsCount++
		g.Titles[entry.Title] = struct{}{}
		if entry.ID != 0 {
			g.MemberIDs[entry.ID] = struct{}{}
		}

		details := detailsMap[entry.ID]
		if details.MediaType == "movie" {
			g.HasMovie = true
		} else {
			g.HasNonMovie = true
		}
	}

	var seriesGroups []groupedView
	var movieGroups []groupedView
	for root, g := range groups {
		avgScore := 0.0
		if g.ItemsCount > 0 {
			avgScore = math.Round((float64(g.TotalScore)/float64(g.ItemsCount))*10) / 10
		}

		memberIDs, err := sortedMemberIDs(g.MemberIDs)
		if err != nil {
			return nil, nil, err
		}
		g.ID = memberIDs[0]
		g.GroupKey = buildGroupKey(memberIDs)

		g.IsIsolatedMovie = false
		if g.ItemsCount == 1 && g.HasMovie && !g.HasNonMovie {
			entry := allEntries[root]
			hasLinkInsideList := false
			for _, relID := range detailsMap[entry.ID].RelatedIDs {
				if len(idToIndexes[relID]) > 0 {
					hasLinkInsideList = true
					break
				}
			}
			g.IsIsolatedMovie = !hasLinkInsideList
		}

		view := groupedView{
			ID:                 g.ID,
			GroupKey:           g.GroupKey,
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       len(g.Titles),
			AvgScore:           avgScore,
			WatchedEpisodesSum: g.NumEpisodesWatched,
		}
		if g.IsIsolatedMovie {
			movieGroups = append(movieGroups, view)
		} else {
			seriesGroups = append(seriesGroups, view)
		}
	}

	sortGroupedViews(seriesGroups)
	sortGroupedViews(movieGroups)
	return seriesGroups, movieGroups, nil
}

func uniqueAnimeDetailsTasks(allEntries []animeEntry) []animeDetailsTask {
	tasks := make([]animeDetailsTask, 0, len(allEntries))
	seen := make(map[int]struct{}, len(allEntries))
	for i, entry := range allEntries {
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		seen[entry.ID] = struct{}{}
		tasks = append(tasks, animeDetailsTask{
			Entry: entry,
			Index: i,
		})
	}
	return tasks
}

func (a *App) runAnimeDetailsPrimaryWorker(
	ctx context.Context,
	workerID int,
	token string,
	cache *animeDetailsCacheStore,
	primaryQueue <-chan animeDetailsTask,
	primaryResults chan<- animeDetailsResult,
	primaryResolver primaryAnimeDetailsResolver,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-primaryQueue:
			if !ok {
				return
			}
			a.logDebug("sync", "resolving anime details in primary worker", "worker", workerID, "id", task.Entry.ID)
			details, err := primaryResolver(token, task.Entry.ID, cache)
			if ctx.Err() != nil {
				return
			}
			primaryResults <- animeDetailsResult{
				Details: details,
				Entry:   task.Entry,
				Err:     err,
				Index:   task.Index,
			}
		}
	}
}

func (a *App) runAnimeDetailsRetryWorker(
	ctx context.Context,
	workerID int,
	token string,
	retryQueue <-chan animeDetailsTask,
	retryResults chan<- animeDetailsResult,
	retryResolver retryAnimeDetailsResolver,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-retryQueue:
			if !ok {
				return
			}
			a.logDebug("sync", "retrying anime details in retry worker", "worker", workerID, "id", task.Entry.ID)
			details, err := retryResolver(token, task.Entry.ID)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				a.logWarn("sync", "background anime details retry failed", "id", task.Entry.ID, "err", err)
			}
			retryResults <- animeDetailsResult{
				Details: details,
				Entry:   task.Entry,
				Err:     err,
				Index:   task.Index,
			}
		}
	}
}

func applyRelatedAnimeLinks(entryIndex int, details animeDetailsInfo, idToIndexes map[int][]int, union func(int, int)) {
	for _, relID := range details.RelatedIDs {
		for _, relatedIndex := range idToIndexes[relID] {
			union(entryIndex, relatedIndex)
		}
	}
}

func summarizeRetryErrors(retryErrors []string) string {
	const maxShown = 3
	if len(retryErrors) <= maxShown {
		return strings.Join(retryErrors, "; ")
	}

	return strings.Join(retryErrors[:maxShown], "; ") + fmt.Sprintf("; and %d more", len(retryErrors)-maxShown)
}

func sortGroupedViews(groups []groupedView) {
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
