package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

func (a *App) runSync(token string) {
	a.logInfo("sync", "MAL sync started")
	if err := a.syncAnime(token); err != nil {
		a.logError("sync", "MAL sync failed", "err", err)
		return
	}
	a.logInfo("sync", "MAL sync completed")
}

func (a *App) syncAnime(token string) error {
	allEntries, err := a.fetchCompletedAnimeEntries(token)
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

	seriesGroups, movieGroups, err := a.groupCompletedAnimeEntries(token, allEntries, cacheStore)
	if err != nil {
		return err
	}

	if err := a.saveGroupedLists(seriesGroups, movieGroups); err != nil {
		return fmt.Errorf("cannot save grouped lists to database: %w", err)
	}

	return nil
}

type primaryAnimeDetailsResolver func(token string, animeID int, cache *animeDetailsCacheStore) (animeDetailsInfo, error)
type retryAnimeDetailsResolver func(token string, animeID int) (animeDetailsInfo, error)

type animeDetailsRetryTask struct {
	Entry animeEntry
	Index int
}

type animeDetailsRetryResult struct {
	Details animeDetailsInfo
	Entry   animeEntry
	Err     error
	Index   int
}

func (a *App) groupCompletedAnimeEntries(token string, allEntries []animeEntry, cache *animeDetailsCacheStore) ([]groupedView, []groupedView, error) {
	return a.groupCompletedAnimeEntriesWithResolvers(token, allEntries, cache, a.fetchAnimeDetailsPrimary, a.fetchAnimeDetailsRetry)
}

func (a *App) groupCompletedAnimeEntriesWithResolvers(
	token string,
	allEntries []animeEntry,
	cache *animeDetailsCacheStore,
	primaryResolver primaryAnimeDetailsResolver,
	retryResolver retryAnimeDetailsResolver,
) ([]groupedView, []groupedView, error) {
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

	retryQueue := make(chan animeDetailsRetryTask, len(allEntries))
	retryResults := make(chan animeDetailsRetryResult, len(allEntries))
	go a.runAnimeDetailsRetryWorker(token, retryQueue, retryResults, retryResolver)

	detailsMap := make(map[int]animeDetailsInfo)
	for i, entry := range allEntries {
		if details, ok := detailsMap[entry.ID]; ok {
			applyRelatedAnimeLinks(i, details, idToIndexes, union)
			continue
		}

		a.logDebug("sync", "resolving anime details", "id", entry.ID)
		details, err := primaryResolver(token, entry.ID, cache)
		if err != nil {
			a.logWarn("sync", "primary anime details lookup failed, queued for retry", "id", entry.ID, "err", err)
			retryQueue <- animeDetailsRetryTask{Entry: entry, Index: i}
			continue
		}

		detailsMap[entry.ID] = details
		applyRelatedAnimeLinks(i, details, idToIndexes, union)
	}

	close(retryQueue)

	var retryErrors []string
	for result := range retryResults {
		if result.Err != nil {
			retryErrors = append(retryErrors, fmt.Sprintf("%d (%s): %v", result.Entry.ID, result.Entry.Title, result.Err))
			continue
		}

		detailsMap[result.Entry.ID] = result.Details
		if err := cache.StoreResolved(result.Entry.ID, result.Details); err != nil {
			a.logWarn("cache", "cannot flush details cache batch", "id", result.Entry.ID, "err", err)
		}
		applyRelatedAnimeLinks(result.Index, result.Details, idToIndexes, union)
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

func (a *App) runAnimeDetailsRetryWorker(
	token string,
	retryQueue <-chan animeDetailsRetryTask,
	retryResults chan<- animeDetailsRetryResult,
	retryResolver retryAnimeDetailsResolver,
) {
	defer close(retryResults)

	for task := range retryQueue {
		a.logDebug("sync", "retrying anime details in background", "id", task.Entry.ID)
		details, err := retryResolver(token, task.Entry.ID)
		if err != nil {
			a.logWarn("sync", "background anime details retry failed", "id", task.Entry.ID, "err", err)
		}
		retryResults <- animeDetailsRetryResult{
			Details: details,
			Entry:   task.Entry,
			Err:     err,
			Index:   task.Index,
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
