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
	NumEpisodes           int
	RelationType          string
	RelationTypeFormatted string
	InUserList            bool
	UserScore             int
	WatchedEpisodes       int
	UserListStatus        string
	MalScore              *float64
}

// AnimeGenre is a MAL genre. ID is the MAL genre id (stable and globally unique
// on MAL); Name is its label. MAL's public anime `genres` field is a single flat
// list mixing genres, themes, and demographics, so they are all represented by
// this type uniformly.
type AnimeGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type AnimeListItem struct {
	ID                 int
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
	SyncedAt           string
	Type               string
	// Pending is true when at least one of the group's owned entries is still an
	// unresolved catalog stub awaiting hydration by the lazy-worker.
	Pending      bool
	StatusCounts map[string]int
	Franchise    []FranchiseEntry
	// Genres is the franchise's aggregated genre set: the union of its members'
	// genres, deduplicated and sorted by name. It is populated only where a
	// caller needs it (the single franchise view); it stays nil otherwise.
	Genres []AnimeGenre
}

// FranchiseSummary is a catalog-wide franchise group reduced to its
// representative title for the "all anime" browse grid. It carries only the
// catalog-backed fields available without a user snapshot, plus the number of
// titles merged into the group.
type FranchiseSummary struct {
	ID             int
	Title          string
	MediaType      string
	StartDate      string
	ImageMediumURL string
	ImageLargeURL  string
	NumEpisodes    int
	MemberCount    int
	// Score is the franchise rating: the average MAL community score over the
	// members that have one. It is nil when no member is scored yet, so the grid
	// can distinguish "unrated" from a real 0.
	Score *float64
}

// FranchiseQuery filters and paginates the catalog-wide franchise listing so the
// "all anime" grid can fetch one page at a time instead of the whole catalog.
// Both filters are optional: a zero MediaType or Search applies no filter, while
// Limit/Offset window the result. The filters match the franchise representative
// (the title shown on the card), not every member of the group.
type FranchiseQuery struct {
	MediaType string // "" = all media types
	Search    string // "" = no title filter
	Limit     int
	Offset    int
}

const (
	AnimeListItemTypeSeries = "series"
	AnimeListItemTypeMovie  = "movie"
	AnimeMediaTypeMovie     = "movie"
)

type AnimeListStatus string

const (
	AnimeListStatusWatching    AnimeListStatus = "watching"
	AnimeListStatusCompleted   AnimeListStatus = "completed"
	AnimeListStatusOnHold      AnimeListStatus = "on_hold"
	AnimeListStatusDropped     AnimeListStatus = "dropped"
	AnimeListStatusPlanToWatch AnimeListStatus = "plan_to_watch"
)

func SupportedAnimeListStatuses() []AnimeListStatus {
	return []AnimeListStatus{
		AnimeListStatusWatching,
		AnimeListStatusCompleted,
		AnimeListStatusOnHold,
		AnimeListStatusDropped,
		AnimeListStatusPlanToWatch,
	}
}

func NormalizeAnimeListStatus(value string) (AnimeListStatus, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")

	switch AnimeListStatus(value) {
	case AnimeListStatusWatching,
		AnimeListStatusCompleted,
		AnimeListStatusOnHold,
		AnimeListStatusDropped,
		AnimeListStatusPlanToWatch:
		return AnimeListStatus(value), true
	default:
		return "", false
	}
}

type AnimeStats struct {
	SeriesCount  int
	MoviesCount  int
	TotalCount   int
	StatusCounts map[string]int
}

type AnimeListGroupInput struct {
	AnimeID               int
	SourceTitle           string
	ListStatus            string
	Score                 int
	WatchedEpisodes       int
	SyncedAt              time.Time
	CatalogTitle          string
	MediaType             string
	FranchiseID           int64
	RepresentativeAnimeID int
	FranchiseDisplayTitle string
	// Resolved reports whether this member's catalog row has been hydrated by the
	// lazy-worker. A false value marks the group as pending (its catalog details
	// — title, image, franchise grouping — have not arrived yet).
	Resolved bool
}

type AnimeListEntry struct {
	Item               AnimeListItem
	GroupMemberIDs     []int
	FranchiseMemberIDs []int
}

type AnimeUserListState struct {
	Score           int
	WatchedEpisodes int
	ListStatus      string
}

type UserAnimeListEntry struct {
	ID                 int
	Title              string
	Score              int
	NumEpisodesWatched int
	ListStatus         AnimeListStatus
}

type AnimeRelation struct {
	ID                    int    `json:"id"`
	Title                 string `json:"title"`
	RelationType          string `json:"relation_type"`
	RelationTypeFormatted string `json:"relation_type_formatted"`
}

type AnimeDetails struct {
	ID              int
	Title           string
	MediaType       string
	StartDate       string
	StartSeasonYear int
	StartSeasonName string
	ImageMediumURL  string
	ImageLargeURL   string
	NumEpisodes     int
	// MalScore is MAL's community mean score. 0 means MAL has no score yet; it is
	// stored as NULL in anime_catalog.mal_score.
	MalScore   float64
	Related    []AnimeRelation
	RelatedIDs []int
	Genres     []AnimeGenre
}

type AnimeCatalogState struct {
	AnimeID         int
	Resolved        bool
	DetailsSyncedAt time.Time
}

type AnimeCatalogSummary struct {
	AnimeID     int
	Title       string
	NumEpisodes int
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

func DeduplicateUserAnimeListEntriesPreserveOrder(entries []UserAnimeListEntry) ([]UserAnimeListEntry, int) {
	deduplicated := make([]UserAnimeListEntry, 0, len(entries))
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

func UniqueUserAnimeListEntryIDs(entries []UserAnimeListEntry) []int {
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

func BuildAnimeListEntries(rows []AnimeListGroupInput, franchiseMemberIDs map[int64][]int) ([]AnimeListEntry, error) {
	type animeListGroup struct {
		key                string
		franchiseID        int64
		representativeID   int
		displayTitle       string
		memberIDs          map[int]struct{}
		titles             map[string]struct{}
		totalScore         int
		scoredItemsCount   int
		itemsCount         int
		watchedEpisodesSum int
		statusCounts       map[string]int
		hasMovie           bool
		hasNonMovie        bool
		pending            bool
		syncedAt           time.Time
	}

	groups := make(map[string]*animeListGroup)
	groupOrder := make([]string, 0)

	for _, row := range rows {
		if row.AnimeID <= 0 {
			continue
		}

		key := fmt.Sprintf("anime:%d", row.AnimeID)
		if row.FranchiseID > 0 {
			key = fmt.Sprintf("franchise:%d", row.FranchiseID)
		}

		group := groups[key]
		if group == nil {
			displayTitle := firstNonEmptyString(row.FranchiseDisplayTitle, row.CatalogTitle, row.SourceTitle)
			if displayTitle == "" {
				displayTitle = fmt.Sprintf("Anime #%d", row.AnimeID)
			}
			representativeID := row.RepresentativeAnimeID
			if representativeID <= 0 {
				representativeID = row.AnimeID
			}

			group = &animeListGroup{
				key:              key,
				franchiseID:      row.FranchiseID,
				representativeID: representativeID,
				displayTitle:     displayTitle,
				memberIDs:        make(map[int]struct{}),
				titles:           make(map[string]struct{}),
				statusCounts:     NewAnimeListStatusCounts(),
				syncedAt:         row.SyncedAt,
			}
			groups[key] = group
			groupOrder = append(groupOrder, key)
		}

		group.memberIDs[row.AnimeID] = struct{}{}
		if title := firstNonEmptyString(row.SourceTitle, row.CatalogTitle); title != "" {
			group.titles[title] = struct{}{}
		}
		if row.Score > 0 {
			group.totalScore += row.Score
			group.scoredItemsCount++
		}
		group.itemsCount++
		group.watchedEpisodesSum += row.WatchedEpisodes
		listStatus, ok := NormalizeAnimeListStatus(row.ListStatus)
		if !ok {
			return nil, fmt.Errorf("unsupported anime list status %q for anime %d", row.ListStatus, row.AnimeID)
		}
		group.statusCounts[string(listStatus)]++
		if row.SyncedAt.After(group.syncedAt) {
			group.syncedAt = row.SyncedAt
		}
		if row.MediaType == AnimeMediaTypeMovie {
			group.hasMovie = true
		} else {
			group.hasNonMovie = true
		}
		if !row.Resolved {
			group.pending = true
		}
	}

	entries := make([]AnimeListEntry, 0, len(groupOrder))
	for _, key := range groupOrder {
		group := groups[key]
		memberIDs, err := SortedMemberIDs(group.memberIDs)
		if err != nil {
			return nil, err
		}

		avgScore := 0.0
		if group.scoredItemsCount > 0 {
			avgScore = RoundScore(float64(group.totalScore) / float64(group.scoredItemsCount))
		}

		mergedTitles := len(group.titles)
		if mergedTitles == 0 {
			mergedTitles = len(memberIDs)
		}

		entry := AnimeListEntry{
			Item: AnimeListItem{
				ID:                 group.representativeID,
				DisplayTitle:       group.displayTitle,
				MergedTitles:       mergedTitles,
				AvgScore:           avgScore,
				WatchedEpisodesSum: group.watchedEpisodesSum,
				SyncedAt:           group.syncedAt.UTC().Format(time.RFC3339),
				Type:               AnimeListItemType(group.itemsCount, group.hasMovie, group.hasNonMovie),
				Pending:            group.pending,
				StatusCounts:       CloneAnimeListStatusCounts(group.statusCounts),
			},
			GroupMemberIDs: memberIDs,
		}
		if group.franchiseID > 0 {
			entry.FranchiseMemberIDs = append([]int(nil), franchiseMemberIDs[group.franchiseID]...)
		}
		entries = append(entries, entry)
	}

	SortAnimeListEntries(entries)
	return entries, nil
}

func AnimeListItemType(itemsCount int, hasMovie, hasNonMovie bool) string {
	if itemsCount == 1 && hasMovie && !hasNonMovie {
		return AnimeListItemTypeMovie
	}
	return AnimeListItemTypeSeries
}

func CountAnimeListStats(entries []AnimeListEntry) AnimeStats {
	stats := AnimeStats{StatusCounts: NewAnimeListStatusCounts()}
	for _, entry := range entries {
		switch entry.Item.Type {
		case AnimeListItemTypeMovie:
			stats.MoviesCount++
		default:
			stats.SeriesCount++
		}
		for status, count := range entry.Item.StatusCounts {
			if _, ok := stats.StatusCounts[status]; ok {
				stats.StatusCounts[status] += count
			}
		}
	}
	stats.TotalCount = stats.SeriesCount + stats.MoviesCount
	return stats
}

func NewAnimeListStatusCounts() map[string]int {
	counts := make(map[string]int, len(SupportedAnimeListStatuses()))
	for _, status := range SupportedAnimeListStatuses() {
		counts[string(status)] = 0
	}
	return counts
}

func CloneAnimeListStatusCounts(counts map[string]int) map[string]int {
	cloned := NewAnimeListStatusCounts()
	for status, count := range counts {
		if _, ok := cloned[status]; ok {
			cloned[status] = count
		}
	}
	return cloned
}

func SortAnimeListEntries(entries []AnimeListEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Item.Type != entries[j].Item.Type {
			return entries[i].Item.Type == AnimeListItemTypeSeries
		}
		return entries[i].Item.ID < entries[j].Item.ID
	})
}

// BuildFranchiseItem assembles a grouped franchise entry from the catalog
// members and the franchise entries already decorated with the caller's
// user-list data. The user-derived aggregates (average score, watched episodes,
// status counts) are computed from that same decoration, so one builder serves
// both the anonymous view (no user states, zeroed aggregates) and the
// authenticated view (the caller's marks), matching the shape produced by
// BuildAnimeListEntries.
func BuildFranchiseItem(
	representativeID int,
	memberIDs []int,
	catalogItems map[int]FranchiseEntry,
	franchise []FranchiseEntry,
) AnimeListItem {
	titles := make(map[string]struct{})
	hasMovie := false
	hasNonMovie := false
	displayTitle := ""

	for _, id := range memberIDs {
		entry, ok := catalogItems[id]
		if !ok {
			continue
		}
		if entry.Title != "" {
			titles[entry.Title] = struct{}{}
		}
		if entry.MediaType == AnimeMediaTypeMovie {
			hasMovie = true
		} else {
			hasNonMovie = true
		}
		if id == representativeID {
			displayTitle = entry.Title
		}
	}

	if displayTitle == "" {
		displayTitle = fmt.Sprintf("Anime #%d", representativeID)
	}

	mergedTitles := len(titles)
	if mergedTitles == 0 {
		mergedTitles = len(memberIDs)
	}

	statusCounts := NewAnimeListStatusCounts()
	totalScore := 0
	scoredItemsCount := 0
	watchedEpisodesSum := 0
	for _, entry := range franchise {
		if !entry.InUserList {
			continue
		}
		if status, ok := NormalizeAnimeListStatus(entry.UserListStatus); ok {
			statusCounts[string(status)]++
		}
		if entry.UserScore > 0 {
			totalScore += entry.UserScore
			scoredItemsCount++
		}
		watchedEpisodesSum += entry.WatchedEpisodes
	}

	avgScore := 0.0
	if scoredItemsCount > 0 {
		avgScore = RoundScore(float64(totalScore) / float64(scoredItemsCount))
	}

	return AnimeListItem{
		ID:                 representativeID,
		DisplayTitle:       displayTitle,
		MergedTitles:       mergedTitles,
		AvgScore:           avgScore,
		WatchedEpisodesSum: watchedEpisodesSum,
		Type:               AnimeListItemType(len(memberIDs), hasMovie, hasNonMovie),
		StatusCounts:       statusCounts,
		Franchise:          franchise,
	}
}

func BuildFranchiseEntries(
	catalogItems map[int]FranchiseEntry,
	userStates map[int]AnimeUserListState,
	relationMap map[int]map[int]AnimeRelation,
	groupMemberIDs []int,
	franchiseIDs []int,
	primaryID int,
) []FranchiseEntry {
	groupMemberIDs = uniquePositiveIDs(groupMemberIDs)
	franchiseIDs = uniquePositiveIDs(franchiseIDs)

	items := make([]FranchiseEntry, 0, len(franchiseIDs))
	for _, animeID := range franchiseIDs {
		item, ok := catalogItems[animeID]
		if !ok {
			continue
		}

		if state, ok := userStates[animeID]; ok {
			item.InUserList = true
			item.UserScore = state.Score
			item.WatchedEpisodes = state.WatchedEpisodes
			item.UserListStatus = state.ListStatus
		}

		// Decorate every entry with how it relates to the franchise except the
		// primary entry itself, which is the reference point and has no relation
		// to itself. Owned entries are decorated the same way as related-only
		// titles so both kinds of cards carry identical content.
		if animeID != primaryID {
			item.RelationType, item.RelationTypeFormatted = pickRelationMetadata(
				animeID,
				groupMemberIDs,
				franchiseIDs,
				relationMap,
			)
		}

		items = append(items, item)
	}

	SortFranchiseEntries(items)
	return items
}

func SortFranchiseEntries(items []FranchiseEntry) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].InUserList != items[j].InUserList {
			return items[i].InUserList && !items[j].InUserList
		}
		if items[i].StartDate == "" && items[j].StartDate != "" {
			return false
		}
		if items[i].StartDate != "" && items[j].StartDate == "" {
			return true
		}
		if items[i].StartDate != items[j].StartDate {
			return items[i].StartDate < items[j].StartDate
		}
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].ID < items[j].ID
	})
}

func pickRelationMetadata(
	targetID int,
	groupMemberIDs []int,
	franchiseIDs []int,
	relationMap map[int]map[int]AnimeRelation,
) (string, string) {
	if relationType, relationTypeFormatted, ok := findRelationMetadata(targetID, groupMemberIDs, relationMap); ok {
		return relationType, relationTypeFormatted
	}

	if relationType, relationTypeFormatted, ok := findRelationMetadata(targetID, franchiseIDs, relationMap); ok {
		return relationType, relationTypeFormatted
	}

	return "", ""
}

func findRelationMetadata(targetID int, sourceIDs []int, relationMap map[int]map[int]AnimeRelation) (string, string, bool) {
	for _, sourceID := range sourceIDs {
		if sourceID == targetID {
			continue
		}
		targets := relationMap[sourceID]
		relation, ok := targets[targetID]
		if !ok {
			continue
		}
		if relation.RelationType != "" || relation.RelationTypeFormatted != "" {
			if relation.RelationTypeFormatted == "" {
				relation.RelationTypeFormatted = FormatAnimeRelationType(relation.RelationType)
			}
			return relation.RelationType, relation.RelationTypeFormatted, true
		}
	}

	return "", "", false
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
	details.Genres = append([]AnimeGenre(nil), details.Genres...)
	return details
}

// EnsureAnimeDetailsGenres normalizes the genres on details in place: it trims
// names, drops entries with a non-positive id or empty name, deduplicates by id
// (keeping the first non-empty name seen), and sorts the result by name. It is
// the genre counterpart of EnsureAnimeDetailsRelatedIDs and is applied at the
// persistence boundary so stored genres are clean and stable.
func EnsureAnimeDetailsGenres(details *AnimeDetails) {
	if details == nil {
		return
	}

	genres := make([]AnimeGenre, 0, len(details.Genres))
	indexByID := make(map[int]int, len(details.Genres))
	for _, genre := range details.Genres {
		genre.Name = strings.TrimSpace(genre.Name)
		if genre.ID <= 0 || genre.Name == "" {
			continue
		}

		index, ok := indexByID[genre.ID]
		if !ok {
			indexByID[genre.ID] = len(genres)
			genres = append(genres, genre)
			continue
		}
		if genres[index].Name == "" {
			genres[index].Name = genre.Name
		}
	}

	sortAnimeGenres(genres)
	details.Genres = genres
}

// AggregateFranchiseGenres returns the union of the genres of the given member
// anime, deduplicated by id and sorted by name. byAnime maps an anime id to its
// genres; memberIDs selects which anime contribute. Members without genres (for
// example unresolved stubs) simply add nothing.
func AggregateFranchiseGenres(byAnime map[int][]AnimeGenre, memberIDs []int) []AnimeGenre {
	if len(byAnime) == 0 || len(memberIDs) == 0 {
		return nil
	}

	genres := make([]AnimeGenre, 0)
	seen := make(map[int]struct{})
	for _, memberID := range memberIDs {
		for _, genre := range byAnime[memberID] {
			if genre.ID <= 0 {
				continue
			}
			if _, ok := seen[genre.ID]; ok {
				continue
			}
			seen[genre.ID] = struct{}{}
			genres = append(genres, genre)
		}
	}

	if len(genres) == 0 {
		return nil
	}

	sortAnimeGenres(genres)
	return genres
}

func sortAnimeGenres(genres []AnimeGenre) {
	sort.Slice(genres, func(i, j int) bool {
		if genres[i].Name != genres[j].Name {
			return genres[i].Name < genres[j].Name
		}
		return genres[i].ID < genres[j].ID
	})
}

func CloneAnimeDetailsBatch(detailsBatch []AnimeDetails) []AnimeDetails {
	cloned := make([]AnimeDetails, 0, len(detailsBatch))
	for _, details := range detailsBatch {
		cloned = append(cloned, CloneAnimeDetails(details))
	}
	return cloned
}
