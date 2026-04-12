package main

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type AnimeSyncLogger struct{}

func (AnimeSyncLogger) Run(db *sql.DB, token string) {
	logInfo("sync", "MAL sync started")
	if err := syncAnime(db, token); err != nil {
		logError("sync", "MAL sync failed", "err", err)
		return
	}
	logInfo("sync", "MAL sync completed")
}

func syncAnime(db *sql.DB, token string) error {

	allEntries, err := fetchCompletedAnimeEntries(token)
	if err != nil {
		return err
	}
	if len(allEntries) == 0 {
		logInfo("sync", "no completed anime found")
		return nil
	}

	cache, err := loadDetailsCache()
	if err != nil {
		logWarn("sync", "cannot load details cache", "err", err)
		cache = map[int]animeDetailsCacheItem{}
	}

	seriesGroups, movieGroups, err := groupCompletedAnimeEntries(token, allEntries, cache)
	if err != nil {
		return err
	}

	if err := saveDetailsCache(cache); err != nil {
		logWarn("sync", "cannot save details cache", "err", err)
	}

	if err := saveGroupedLists(db, seriesGroups, movieGroups); err != nil {
		return fmt.Errorf("cannot save grouped lists to database: %w", err)
	}

	return nil
}

func groupCompletedAnimeEntries(token string, allEntries []animeEntry, cache map[int]animeDetailsCacheItem) ([]groupedView, []groupedView, error) {
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

	detailsMap := make(map[int]animeDetailsInfo)
	for i, entry := range allEntries {
		logDebug("sync", "resolving anime details", "anime_id", entry.ID, "title", entry.Title)
		details, err := fetchAnimeDetails(token, entry.ID, cache)
		if err != nil {
			return nil, nil, err
		}

		detailsMap[entry.ID] = details
		for _, relID := range details.RelatedIDs {
			for _, j := range idToIndexes[relID] {
				union(i, j)
			}
		}
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
