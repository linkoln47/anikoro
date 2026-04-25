package domain

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FranchiseEntry struct {
	ID                    int
	Title                 string
	MediaType             string
	StartDate             string
	ImageMediumURL        string
	ImageLargeURL         string
	RelationType          string
	RelationTypeFormatted string
	InUserList            bool
	UserScore             int
	WatchedEpisodes       int
}

type AnimeListItem struct {
	ID                 int
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
	SyncedAt           string
	Type               string
	Franchise          []FranchiseEntry
}

type AnimeStats struct {
	SeriesCount int
	MoviesCount int
	TotalCount  int
}

type CompletedAnimeEntry struct {
	ID                 int
	Title              string
	Score              int
	NumEpisodesWatched int
}

type AnimeRelation struct {
	ID                    int    `json:"id"`
	Title                 string `json:"title"`
	RelationType          string `json:"relation_type"`
	RelationTypeFormatted string `json:"relation_type_formatted"`
}

type AnimeDetails struct {
	ID             int
	Title          string
	MediaType      string
	StartDate      string
	ImageMediumURL string
	ImageLargeURL  string
	Related        []AnimeRelation
	RelatedIDs     []int
}

type AnimeCatalogState struct {
	AnimeID         int
	Resolved        bool
	DetailsSyncedAt time.Time
}

type GroupedView struct {
	ID                 int
	GroupKey           string
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	GroupMemberIDs     []int
	WatchedEpisodesSum int
}

func DeduplicateCompletedAnimeEntriesPreserveOrder(entries []CompletedAnimeEntry) ([]CompletedAnimeEntry, int) {
	deduplicated := make([]CompletedAnimeEntry, 0, len(entries))
	seen := make(map[int]struct{}, len(entries))
	duplicateCount := 0

	for _, entry := range entries {
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

func UniqueCompletedAnimeIDs(entries []CompletedAnimeEntry) []int {
	ids := make([]int, 0, len(entries))
	seen := make(map[int]struct{}, len(entries))
	for _, entry := range entries {
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

func RoundScore(score float64) float64 {
	return math.Round(score*10) / 10
}

func SortGroupedViews(groups []GroupedView) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].WatchedEpisodesSum == groups[j].WatchedEpisodesSum {
			return groups[i].DisplayTitle < groups[j].DisplayTitle
		}
		return groups[i].WatchedEpisodesSum > groups[j].WatchedEpisodesSum
	})
}

func SortedMemberIDs(memberIDs map[int]struct{}) ([]int, error) {
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

func BuildGroupKey(memberIDs []int) string {
	parts := make([]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ":")
}

func IsTraversableAnimeRelationType(relationType string) bool {
	switch strings.ToLower(strings.TrimSpace(relationType)) {
	case "character", "other":
		return false
	default:
		return true
	}
}

func CollectTraversableRelatedIDs(details AnimeDetails) []int {
	normalized := CloneAnimeDetails(details)
	EnsureAnimeDetailsRelatedIDs(&normalized)

	relatedIDs := make([]int, 0, len(normalized.Related))
	for _, relation := range normalized.Related {
		if relation.ID <= 0 || !IsTraversableAnimeRelationType(relation.RelationType) {
			continue
		}
		relatedIDs = append(relatedIDs, relation.ID)
	}

	return relatedIDs
}

func FormatAnimeRelationType(relationType string) string {
	relationType = strings.TrimSpace(relationType)
	if relationType == "" {
		return ""
	}

	label := strings.ToLower(strings.ReplaceAll(relationType, "_", " "))
	return strings.ToUpper(label[:1]) + label[1:]
}

func EnsureAnimeDetailsRelatedIDs(details *AnimeDetails) {
	if details == nil {
		return
	}

	related := make([]AnimeRelation, 0, len(details.Related)+len(details.RelatedIDs))
	relatedIndexByID := make(map[int]int, len(details.Related)+len(details.RelatedIDs))
	mergeRelated := func(candidate AnimeRelation) {
		if candidate.ID <= 0 {
			return
		}

		index, ok := relatedIndexByID[candidate.ID]
		if !ok {
			relatedIndexByID[candidate.ID] = len(related)
			related = append(related, candidate)
			return
		}

		existing := &related[index]
		if existing.Title == "" && candidate.Title != "" {
			existing.Title = candidate.Title
		}
		if existing.RelationType == "" && candidate.RelationType != "" {
			existing.RelationType = candidate.RelationType
		}
		if existing.RelationTypeFormatted == "" && candidate.RelationTypeFormatted != "" {
			existing.RelationTypeFormatted = candidate.RelationTypeFormatted
		}
	}

	for _, relation := range details.Related {
		mergeRelated(relation)
	}
	for _, relatedID := range details.RelatedIDs {
		mergeRelated(AnimeRelation{ID: relatedID})
	}

	relatedIDs := make([]int, 0, len(related))
	for _, relation := range related {
		relatedIDs = append(relatedIDs, relation.ID)
	}

	details.Related = related
	details.RelatedIDs = relatedIDs
}

func CloneAnimeDetails(details AnimeDetails) AnimeDetails {
	details.Related = append([]AnimeRelation(nil), details.Related...)
	details.RelatedIDs = append([]int(nil), details.RelatedIDs...)
	return details
}

func CloneAnimeDetailsBatch(detailsBatch []AnimeDetails) []AnimeDetails {
	cloned := make([]AnimeDetails, 0, len(detailsBatch))
	for _, details := range detailsBatch {
		cloned = append(cloned, CloneAnimeDetails(details))
	}
	return cloned
}
