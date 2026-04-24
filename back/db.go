package main

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	animeFranchisesTableName          = "anime_franchises"
	animeFranchiseMembersTableName    = "anime_franchise_members"
	usersTableName                    = "users"
	malTokensTable                    = "mal_tokens"
	userScopeSetting                  = "app.user_id"
	defaultDBTimeout                  = 5 * time.Second
	defaultMaxOpenDB                  = 10
	defaultMaxIdleDB                  = 5
	traversableAnimeRelationFilterSQL = "COALESCE(LOWER(relation_type), '') NOT IN ('character', 'other')"
)

type User struct {
	ID        int64
	MALUserID int64
	Username  string
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

type animeCatalogState struct {
	AnimeID         int
	Resolved        bool
	DetailsSyncedAt time.Time
}

type animeListEntrySnapshot struct {
	Item               AnimeItem
	GroupMemberIDs     []int
	FranchiseMemberIDs []int
}

type userAnimeItemState struct {
	Score           int
	WatchedEpisodes int
}

func roundScore(score float64) float64 {
	return math.Round(score*10) / 10
}

func (a *App) ListAnime(userID int64) ([]AnimeItem, error) {
	anime := make([]AnimeItem, 0)

	err := a.withUserTx(context.Background(), userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entrySnapshots, err := a.listAnimeEntrySnapshotsWithContext(context.Background(), tx, userID)
		if err != nil {
			return err
		}

		for _, snapshot := range entrySnapshots {
			item := snapshot.Item
			item.Franchise = []FranchiseItem{}

			if len(snapshot.FranchiseMemberIDs) > 0 {
				item.Franchise, err = a.buildFranchiseItemsWithContext(
					context.Background(),
					tx,
					userID,
					snapshot.GroupMemberIDs,
					snapshot.FranchiseMemberIDs,
				)
				if err != nil {
					return fmt.Errorf("build franchise for anime %d: %w", item.ID, err)
				}
			}

			anime = append(anime, item)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return anime, nil
}

func (a *App) listAnimeEntrySnapshotsWithContext(ctx context.Context, tx *sql.Tx, userID int64) ([]animeListEntrySnapshot, error) {
	ctx = ensureContext(ctx)

	rows, err := tx.QueryContext(ctx, `
		SELECT
			ui.anime_id,
			ui.source_title,
			ui.score,
			ui.watched_episodes,
			ui.synced_at,
			COALESCE(ac.title, ''),
			COALESCE(ac.media_type, ''),
			COALESCE(fm.franchise_id, 0),
			COALESCE(fr.representative_anime_id, ui.anime_id),
			COALESCE(frac.title, '')
		FROM user_anime_items ui
		LEFT JOIN anime_catalog ac ON ac.id = ui.anime_id
		LEFT JOIN anime_franchise_members fm ON fm.anime_id = ui.anime_id
		LEFT JOIN (
			SELECT franchise_id, MIN(anime_id) AS representative_anime_id
			FROM anime_franchise_members
			GROUP BY franchise_id
		) fr ON fr.franchise_id = fm.franchise_id
		LEFT JOIN anime_catalog frac ON frac.id = fr.representative_anime_id
		WHERE ui.user_id = $1
		ORDER BY COALESCE(fm.franchise_id, 0), ui.anime_id
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query anime entries: %w", err)
	}
	defer rows.Close()

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
		hasMovie           bool
		hasNonMovie        bool
		syncedAt           time.Time
	}

	groups := make(map[string]*animeListGroup)
	groupOrder := make([]string, 0)
	franchiseIDs := make([]int64, 0)

	for rows.Next() {
		var (
			animeID               int
			sourceTitle           string
			score                 int
			watchedEpisodes       int
			syncedAt              time.Time
			catalogTitle          string
			mediaType             string
			franchiseID           int64
			representativeAnimeID int
			franchiseDisplayTitle string
		)
		if err := rows.Scan(
			&animeID,
			&sourceTitle,
			&score,
			&watchedEpisodes,
			&syncedAt,
			&catalogTitle,
			&mediaType,
			&franchiseID,
			&representativeAnimeID,
			&franchiseDisplayTitle,
		); err != nil {
			return nil, fmt.Errorf("scan anime entries row: %w", err)
		}

		if animeID <= 0 {
			continue
		}

		key := fmt.Sprintf("anime:%d", animeID)
		if franchiseID > 0 {
			key = fmt.Sprintf("franchise:%d", franchiseID)
		}

		group := groups[key]
		if group == nil {
			displayTitle := firstNonEmpty(franchiseDisplayTitle, catalogTitle, sourceTitle)
			if displayTitle == "" {
				displayTitle = fmt.Sprintf("Anime #%d", animeID)
			}
			if representativeAnimeID <= 0 {
				representativeAnimeID = animeID
			}

			group = &animeListGroup{
				key:              key,
				franchiseID:      franchiseID,
				representativeID: representativeAnimeID,
				displayTitle:     displayTitle,
				memberIDs:        make(map[int]struct{}),
				titles:           make(map[string]struct{}),
				syncedAt:         syncedAt,
			}
			groups[key] = group
			groupOrder = append(groupOrder, key)
			if franchiseID > 0 {
				franchiseIDs = append(franchiseIDs, franchiseID)
			}
		}

		group.memberIDs[animeID] = struct{}{}
		if title := firstNonEmpty(sourceTitle, catalogTitle); title != "" {
			group.titles[title] = struct{}{}
		}
		if score > 0 {
			group.totalScore += score
			group.scoredItemsCount++
		}
		group.itemsCount++
		group.watchedEpisodesSum += watchedEpisodes
		if syncedAt.After(group.syncedAt) {
			group.syncedAt = syncedAt
		}
		if mediaType == "movie" {
			group.hasMovie = true
		} else {
			group.hasNonMovie = true
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate anime entries rows: %w", err)
	}

	franchiseMemberIDs, err := listAnimeFranchiseMemberIDsByFranchiseIDsWithContext(ctx, tx, franchiseIDs)
	if err != nil {
		return nil, err
	}

	entries := make([]animeListEntrySnapshot, 0, len(groupOrder))
	for _, key := range groupOrder {
		group := groups[key]
		memberIDs, err := sortedMemberIDs(group.memberIDs)
		if err != nil {
			return nil, err
		}

		avgScore := 0.0
		if group.scoredItemsCount > 0 {
			avgScore = roundScore(float64(group.totalScore) / float64(group.scoredItemsCount))
		}

		itemType := "series"
		if group.itemsCount == 1 && group.hasMovie && !group.hasNonMovie {
			itemType = "movie"
		}

		mergedTitles := len(group.titles)
		if mergedTitles == 0 {
			mergedTitles = len(memberIDs)
		}

		entry := animeListEntrySnapshot{
			Item: AnimeItem{
				ID:                 group.representativeID,
				DisplayTitle:       group.displayTitle,
				MergedTitles:       mergedTitles,
				AvgScore:           avgScore,
				WatchedEpisodesSum: group.watchedEpisodesSum,
				SyncedAt:           group.syncedAt.UTC().Format(time.RFC3339),
				Type:               itemType,
			},
			GroupMemberIDs: memberIDs,
		}
		if group.franchiseID > 0 {
			entry.FranchiseMemberIDs = append([]int(nil), franchiseMemberIDs[group.franchiseID]...)
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Item.Type != entries[j].Item.Type {
			return entries[i].Item.Type == "series"
		}
		return entries[i].Item.ID < entries[j].Item.ID
	})

	return entries, nil
}

func (a *App) collectFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, seedIDs []int) ([]int, error) {
	ctx = ensureContext(ctx)

	componentIDs := make(map[int]struct{}, maxNodesPerFranchise)
	queue := uniquePositiveIDs(seedIDs)

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
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
			break
		}

		componentIDs[animeID] = struct{}{}
		relatedIDs, err := listUndirectedAnimeRelationIDsWithContext(ctx, tx, animeID)
		if err != nil {
			return nil, err
		}
		queue = append(queue, relatedIDs...)
	}

	ids := make([]int, 0, len(componentIDs))
	for id := range componentIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}

func (a *App) buildFranchiseItemsWithContext(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	groupMemberIDs []int,
	franchiseIDs []int,
) ([]FranchiseItem, error) {
	ctx = ensureContext(ctx)

	franchiseIDs = uniquePositiveIDs(franchiseIDs)
	groupMemberIDs = uniquePositiveIDs(groupMemberIDs)

	catalogItems, err := listCatalogItemsByIDsWithContext(ctx, tx, franchiseIDs)
	if err != nil {
		return nil, err
	}

	userStates, err := listUserAnimeItemsByIDsWithContext(ctx, tx, userID, franchiseIDs)
	if err != nil {
		return nil, err
	}

	relationMap, err := listRelationsBySourceIDsWithContext(ctx, tx, franchiseIDs)
	if err != nil {
		return nil, err
	}

	groupMemberSet := make(map[int]struct{}, len(groupMemberIDs))
	for _, memberID := range groupMemberIDs {
		groupMemberSet[memberID] = struct{}{}
	}

	items := make([]FranchiseItem, 0, len(franchiseIDs))
	for _, animeID := range franchiseIDs {
		item, ok := catalogItems[animeID]
		if !ok {
			continue
		}

		if state, ok := userStates[animeID]; ok {
			item.InUserList = true
			item.UserScore = state.Score
			item.WatchedEpisodes = state.WatchedEpisodes
		}

		if _, ok := groupMemberSet[animeID]; !ok {
			item.RelationType, item.RelationTypeFormatted = pickRelationMetadata(
				animeID,
				groupMemberIDs,
				franchiseIDs,
				relationMap,
			)
		}

		items = append(items, item)
	}

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

	return items, nil
}

func listCatalogItemsByIDsWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) (map[int]FranchiseItem, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]FranchiseItem{}, nil
	}

	args := intsToAnySlice(animeIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id,
			COALESCE(title, ''),
			COALESCE(media_type, ''),
			COALESCE(start_date::text, ''),
			COALESCE(img_small_url, ''),
			COALESCE(img_large_url, '')
		FROM anime_catalog
		WHERE id IN (%s)
	`, buildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[int]FranchiseItem, len(animeIDs))
	for rows.Next() {
		var item FranchiseItem
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
		); err != nil {
			return nil, err
		}
		items[item.ID] = item
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func listUserAnimeItemsByIDsWithContext(ctx context.Context, tx *sql.Tx, userID int64, animeIDs []int) (map[int]userAnimeItemState, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]userAnimeItemState{}, nil
	}

	args := make([]any, 0, len(animeIDs)+1)
	args = append(args, userID)
	args = append(args, intsToAnySlice(animeIDs)...)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT anime_id, COALESCE(score, 0), watched_episodes
		FROM user_anime_items
		WHERE user_id = $1
			AND anime_id IN (%s)
	`, buildSQLPlaceholders(2, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[int]userAnimeItemState, len(animeIDs))
	for rows.Next() {
		var (
			animeID int
			state   userAnimeItemState
		)
		if err := rows.Scan(&animeID, &state.Score, &state.WatchedEpisodes); err != nil {
			return nil, err
		}
		items[animeID] = state
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func listAnimeFranchiseMemberIDsByFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, franchiseIDs []int64) (map[int64][]int, error) {
	ctx = ensureContext(ctx)

	franchiseIDs = uniquePositiveInt64s(franchiseIDs)
	if len(franchiseIDs) == 0 {
		return map[int64][]int{}, nil
	}

	args := int64sToAnySlice(franchiseIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT franchise_id, anime_id
		FROM anime_franchise_members
		WHERE franchise_id IN (%s)
		ORDER BY franchise_id, anime_id
	`, buildSQLPlaceholders(1, len(franchiseIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memberIDs := make(map[int64][]int, len(franchiseIDs))
	for rows.Next() {
		var (
			franchiseID int64
			animeID     int
		)
		if err := rows.Scan(&franchiseID, &animeID); err != nil {
			return nil, err
		}
		memberIDs[franchiseID] = append(memberIDs[franchiseID], animeID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return memberIDs, nil
}

func listRelationsBySourceIDsWithContext(ctx context.Context, tx *sql.Tx, sourceIDs []int) (map[int]map[int]AnimeRelationInfo, error) {
	ctx = ensureContext(ctx)

	sourceIDs = uniquePositiveIDs(sourceIDs)
	if len(sourceIDs) == 0 {
		return map[int]map[int]AnimeRelationInfo{}, nil
	}

	args := intsToAnySlice(sourceIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id,
			related_id,
			COALESCE(relation_type, '')
		FROM anime_relations
		WHERE id IN (%s)
	`, buildSQLPlaceholders(1, len(sourceIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationMap := make(map[int]map[int]AnimeRelationInfo, len(sourceIDs))
	for rows.Next() {
		var (
			sourceID int
			relation AnimeRelationInfo
		)
		if err := rows.Scan(
			&sourceID,
			&relation.ID,
			&relation.RelationType,
		); err != nil {
			return nil, err
		}
		relation.RelationTypeFormatted = formatAnimeRelationType(relation.RelationType)

		targets := relationMap[sourceID]
		if targets == nil {
			targets = make(map[int]AnimeRelationInfo)
			relationMap[sourceID] = targets
		}
		targets[relation.ID] = relation
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relationMap, nil
}

func pickRelationMetadata(
	targetID int,
	groupMemberIDs []int,
	franchiseIDs []int,
	relationMap map[int]map[int]AnimeRelationInfo,
) (string, string) {
	if relationType, relationTypeFormatted, ok := findRelationMetadata(targetID, groupMemberIDs, relationMap); ok {
		return relationType, relationTypeFormatted
	}

	if relationType, relationTypeFormatted, ok := findRelationMetadata(targetID, franchiseIDs, relationMap); ok {
		return relationType, relationTypeFormatted
	}

	return "", ""
}

func findRelationMetadata(targetID int, sourceIDs []int, relationMap map[int]map[int]AnimeRelationInfo) (string, string, bool) {
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
				relation.RelationTypeFormatted = formatAnimeRelationType(relation.RelationType)
			}
			return relation.RelationType, relation.RelationTypeFormatted, true
		}
	}

	return "", "", false
}

func listUndirectedAnimeRelationIDsWithContext(ctx context.Context, tx *sql.Tx, animeID int) ([]int, error) {
	ctx = ensureContext(ctx)

	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT neighbor_id
		FROM (
			SELECT related_id AS neighbor_id, COALESCE(LOWER(relation_type), '') AS relation_type
			FROM anime_relations
			WHERE id = $1
			UNION ALL
			SELECT id AS neighbor_id, COALESCE(LOWER(relation_type), '') AS relation_type
			FROM anime_relations
			WHERE related_id = $1
		) AS neighbors
		WHERE neighbor_id <> $1
			AND relation_type NOT IN ('character', 'other')
		ORDER BY neighbor_id
	`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relatedIDs := make([]int, 0)
	for rows.Next() {
		var relatedID int
		if err := rows.Scan(&relatedID); err != nil {
			return nil, err
		}
		relatedIDs = append(relatedIDs, relatedID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relatedIDs, nil
}

func (a *App) GetStats(userID int64) (StatsResponse, error) {
	var stats StatsResponse

	err := a.withUserTx(context.Background(), userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entries, err := a.listAnimeEntrySnapshotsWithContext(context.Background(), tx, userID)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			switch entry.Item.Type {
			case "movie":
				stats.MoviesCount++
			default:
				stats.SeriesCount++
			}
		}
		return nil
	})
	if err != nil {
		return StatsResponse{}, err
	}

	stats.TotalCount = stats.SeriesCount + stats.MoviesCount
	return stats, nil
}

func openDB(cfg AppConfig) (*sql.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	db.SetMaxOpenConns(defaultMaxOpenDB)
	db.SetMaxIdleConns(defaultMaxIdleDB)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}

	return db, nil
}

func (a *App) withTx(ctx context.Context, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	ctx = ensureContext(ctx)

	tx, err := a.DB.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func (a *App) withUserTx(ctx context.Context, userID int64, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	ctx = ensureContext(ctx)

	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}

	tx, err := a.DB.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := setUserScope(ctx, tx, userID); err != nil {
		return fmt.Errorf("set user scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func setUserScope(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('`+userScopeSetting+`', $1, true)`, strconv.FormatInt(userID, 10))
	return err
}

func (a *App) upsertAnimeCatalogStubsWithContext(ctx context.Context, animeIDs []int) error {
	ctx = ensureContext(ctx)
	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return nil
	}

	return a.withTx(ctx, nil, func(tx *sql.Tx) error {
		return upsertAnimeCatalogStubsWithTx(ctx, tx, animeIDs)
	})
}

type animeFranchiseComponent struct {
	MemberIDs []int
	MemberKey string
}

func (a *App) refreshAnimeFranchisesWithContext(ctx context.Context, seedIDs []int) error {
	ctx = ensureContext(ctx)

	seedIDs = uniquePositiveIDs(seedIDs)
	if len(seedIDs) == 0 {
		return nil
	}

	return a.withTx(ctx, nil, func(tx *sql.Tx) error {
		a.logInfo("db", "refreshing global anime franchises", "table", animeFranchisesTableName, "seed_count", len(seedIDs))

		worklist := append([]int(nil), seedIDs...)
		impactedFranchiseIDs, err := listAnimeFranchiseIDsByAnimeIDsWithContext(ctx, tx, worklist)
		if err != nil {
			return err
		}
		oldMemberIDs, err := listAnimeFranchiseMemberIDsByFranchiseIDsWithContext(ctx, tx, impactedFranchiseIDs)
		if err != nil {
			return err
		}
		for _, memberIDs := range oldMemberIDs {
			worklist = append(worklist, memberIDs...)
		}
		worklist = uniquePositiveIDs(worklist)

		coveredAnimeIDs := make(map[int]struct{}, len(worklist))
		seenKeys := make(map[string]struct{}, len(worklist))
		components := make([]animeFranchiseComponent, 0, len(worklist))
		for _, seedID := range worklist {
			if _, ok := coveredAnimeIDs[seedID]; ok {
				continue
			}

			memberIDs, err := a.collectFranchiseIDsWithContext(ctx, tx, []int{seedID})
			if err != nil {
				return err
			}
			if len(memberIDs) == 0 {
				memberIDs = []int{seedID}
			}
			for _, memberID := range memberIDs {
				coveredAnimeIDs[memberID] = struct{}{}
			}

			memberKey := buildGroupKey(memberIDs)
			if _, ok := seenKeys[memberKey]; ok {
				continue
			}
			seenKeys[memberKey] = struct{}{}

			existingFranchiseIDs, err := listAnimeFranchiseIDsByAnimeIDsWithContext(ctx, tx, memberIDs)
			if err != nil {
				return err
			}
			impactedFranchiseIDs = append(impactedFranchiseIDs, existingFranchiseIDs...)

			components = append(components, animeFranchiseComponent{
				MemberIDs: memberIDs,
				MemberKey: memberKey,
			})
		}

		if err := deleteAnimeFranchiseMembersByFranchiseIDsWithContext(ctx, tx, impactedFranchiseIDs); err != nil {
			return err
		}
		for _, component := range components {
			franchiseID, err := upsertAnimeFranchiseWithContext(ctx, tx, component)
			if err != nil {
				return err
			}
			if err := upsertAnimeFranchiseMembersWithContext(ctx, tx, franchiseID, component.MemberIDs); err != nil {
				return err
			}
		}

		return deleteOrphanAnimeFranchisesWithContext(ctx, tx)
	})
}

func listAnimeFranchiseIDsByAnimeIDsWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) ([]int64, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return nil, nil
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT franchise_id
		FROM anime_franchise_members
		WHERE anime_id IN (%s)
		ORDER BY franchise_id
	`, buildSQLPlaceholders(1, len(animeIDs))), intsToAnySlice(animeIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	franchiseIDs := make([]int64, 0)
	for rows.Next() {
		var franchiseID int64
		if err := rows.Scan(&franchiseID); err != nil {
			return nil, err
		}
		franchiseIDs = append(franchiseIDs, franchiseID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return franchiseIDs, nil
}

func deleteAnimeFranchiseMembersByFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, franchiseIDs []int64) error {
	ctx = ensureContext(ctx)

	franchiseIDs = uniquePositiveInt64s(franchiseIDs)
	if len(franchiseIDs) == 0 {
		return nil
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM anime_franchise_members
		WHERE franchise_id IN (%s)
	`, buildSQLPlaceholders(1, len(franchiseIDs))), int64sToAnySlice(franchiseIDs)...)
	return err
}

func upsertAnimeFranchiseWithContext(ctx context.Context, tx *sql.Tx, component animeFranchiseComponent) (int64, error) {
	ctx = ensureContext(ctx)

	var franchiseID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO anime_franchises (
			member_key,
			created_at,
			updated_at
		) VALUES ($1, NOW(), NOW())
		ON CONFLICT (member_key) DO UPDATE
		SET
			updated_at = NOW()
		RETURNING id
	`, component.MemberKey).Scan(&franchiseID)
	return franchiseID, err
}

func upsertAnimeFranchiseMembersWithContext(ctx context.Context, tx *sql.Tx, franchiseID int64, animeIDs []int) error {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if franchiseID <= 0 || len(animeIDs) == 0 {
		return nil
	}

	rows := make([]string, 0, len(animeIDs))
	args := make([]any, 0, len(animeIDs)*2)
	argIndex := 1
	for _, animeID := range animeIDs {
		rows = append(rows, fmt.Sprintf("($%d, $%d)", argIndex, argIndex+1))
		args = append(args, animeID, franchiseID)
		argIndex += 2
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO anime_franchise_members (
			anime_id,
			franchise_id
		) VALUES %s
		ON CONFLICT (anime_id) DO UPDATE
		SET franchise_id = EXCLUDED.franchise_id
	`, strings.Join(rows, ", ")), args...)
	return err
}

func deleteOrphanAnimeFranchisesWithContext(ctx context.Context, tx *sql.Tx) error {
	ctx = ensureContext(ctx)

	_, err := tx.ExecContext(ctx, `
		DELETE FROM anime_franchises f
		WHERE NOT EXISTS (
			SELECT 1
			FROM anime_franchise_members fm
			WHERE fm.franchise_id = f.id
		)
	`)
	return err
}

func (a *App) replaceUserAnimeItemsWithContext(ctx context.Context, userID int64, entries []AnimeEntry) error {
	ctx = ensureContext(ctx)

	return a.withUserTx(ctx, userID, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items WHERE user_id = $1`, userID); err != nil {
			return err
		}

		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO user_anime_items (
				user_id,
				anime_id,
				source_title,
				score,
				watched_episodes,
				synced_at
			) VALUES ($1, $2, $3, $4, $5, $6)
		`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		syncedAt := time.Now().UTC()
		for _, entry := range entries {
			if entry.ID <= 0 {
				continue
			}
			if _, err := stmt.ExecContext(
				ctx,
				userID,
				entry.ID,
				entry.Title,
				entry.Score,
				entry.NumEpisodesWatched,
				syncedAt,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

func (a *App) clearUserAnimeSnapshotWithContext(ctx context.Context, userID int64) error {
	ctx = ensureContext(ctx)

	return a.withUserTx(ctx, userID, nil, func(tx *sql.Tx) error {
		a.logInfo("db", "clearing empty user anime snapshot", "user_id", userID)

		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items WHERE user_id = $1`, userID); err != nil {
			return err
		}

		return nil
	})
}

func (a *App) getAnimeCatalogStateWithContext(ctx context.Context, animeID int) (animeCatalogState, bool, error) {
	ctx = ensureContext(ctx)

	var state animeCatalogState
	err := a.DB.QueryRowContext(ctx, `
		SELECT id, resolved, COALESCE(details_synced_at, TIMESTAMPTZ 'epoch')
		FROM anime_catalog
		WHERE id = $1
	`, animeID).Scan(&state.AnimeID, &state.Resolved, &state.DetailsSyncedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return animeCatalogState{}, false, nil
		}
		return animeCatalogState{}, false, err
	}

	return state, true, nil
}

func (a *App) getAnimeCatalogStatesByIDsWithContext(ctx context.Context, animeIDs []int) (map[int]animeCatalogState, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]animeCatalogState{}, nil
	}

	args := intsToAnySlice(animeIDs)
	rows, err := a.DB.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, resolved, COALESCE(details_synced_at, TIMESTAMPTZ 'epoch')
		FROM anime_catalog
		WHERE id IN (%s)
	`, buildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[int]animeCatalogState, len(animeIDs))
	for rows.Next() {
		var state animeCatalogState
		if err := rows.Scan(&state.AnimeID, &state.Resolved, &state.DetailsSyncedAt); err != nil {
			return nil, err
		}
		states[state.AnimeID] = state
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return states, nil
}

func (a *App) getAnimeCatalogMediaTypeWithContext(ctx context.Context, animeID int) (string, error) {
	ctx = ensureContext(ctx)

	var mediaType sql.NullString
	err := a.DB.QueryRowContext(ctx, `
		SELECT media_type
		FROM anime_catalog
		WHERE id = $1
	`, animeID).Scan(&mediaType)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}

	return mediaType.String, nil
}

func (a *App) replaceAnimeRelationsWithContext(ctx context.Context, animeID int, relations []AnimeRelationInfo) error {
	ctx = ensureContext(ctx)

	return a.withTx(ctx, nil, func(tx *sql.Tx) error {
		return replaceAnimeRelationsBatchWithTx(ctx, tx, []AnimeDetailsInfo{{
			ID:      animeID,
			Related: append([]AnimeRelationInfo(nil), relations...),
		}})
	})
}

func (a *App) saveAnimeCatalogDetailsWithContext(ctx context.Context, details AnimeDetailsInfo) error {
	ctx = ensureContext(ctx)

	return a.saveAnimeCatalogDetailsBatchWithContext(ctx, []AnimeDetailsInfo{details})
}

func (a *App) saveAnimeCatalogDetailsBatchWithContext(ctx context.Context, detailsBatch []AnimeDetailsInfo) error {
	ctx = ensureContext(ctx)

	normalized, err := normalizeAnimeCatalogDetailsBatch(detailsBatch)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}

	return a.withTx(ctx, nil, func(tx *sql.Tx) error {
		if err := upsertAnimeCatalogStubsWithTx(ctx, tx, collectAnimeCatalogStubIDs(normalized)); err != nil {
			return err
		}
		syncedAt := time.Now().UTC()
		if err := upsertAnimeCatalogDetailsBatchWithTx(ctx, tx, normalized, syncedAt); err != nil {
			return err
		}
		return replaceAnimeRelationsBatchWithTx(ctx, tx, normalized)
	})
}

func (a *App) listAnimeRelationIDsWithContext(ctx context.Context, animeID int) ([]int, error) {
	ctx = ensureContext(ctx)

	rows, err := a.DB.QueryContext(ctx, `
		SELECT related_id
		FROM anime_relations
		WHERE id = $1
			AND `+traversableAnimeRelationFilterSQL+`
		ORDER BY related_id
	`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relatedIDs := make([]int, 0)
	for rows.Next() {
		var relatedID int
		if err := rows.Scan(&relatedID); err != nil {
			return nil, err
		}
		relatedIDs = append(relatedIDs, relatedID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relatedIDs, nil
}

func (a *App) listAnimeRelationIDsBySourceIDsWithContext(ctx context.Context, animeIDs []int) (map[int][]int, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int][]int{}, nil
	}

	args := intsToAnySlice(animeIDs)
	rows, err := a.DB.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, related_id
		FROM anime_relations
		WHERE id IN (%s)
			AND %s
		ORDER BY id, related_id
	`, buildSQLPlaceholders(1, len(animeIDs)), traversableAnimeRelationFilterSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relatedIDsBySource := make(map[int][]int, len(animeIDs))
	for rows.Next() {
		var (
			sourceID  int
			relatedID int
		)
		if err := rows.Scan(&sourceID, &relatedID); err != nil {
			return nil, err
		}
		relatedIDsBySource[sourceID] = append(relatedIDsBySource[sourceID], relatedID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relatedIDsBySource, nil
}

func (a *App) listUndirectedAnimeRelationIDsWithContext(ctx context.Context, animeID int) ([]int, error) {
	ctx = ensureContext(ctx)

	rows, err := a.DB.QueryContext(ctx, `
		SELECT DISTINCT neighbor_id
		FROM (
			SELECT related_id AS neighbor_id, COALESCE(LOWER(relation_type), '') AS relation_type
			FROM anime_relations
			WHERE id = $1
			UNION ALL
			SELECT id AS neighbor_id, COALESCE(LOWER(relation_type), '') AS relation_type
			FROM anime_relations
			WHERE related_id = $1
		) AS neighbors
		WHERE neighbor_id <> $1
			AND relation_type NOT IN ('character', 'other')
		ORDER BY neighbor_id
	`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relatedIDs := make([]int, 0)
	for rows.Next() {
		var relatedID int
		if err := rows.Scan(&relatedID); err != nil {
			return nil, err
		}
		relatedIDs = append(relatedIDs, relatedID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relatedIDs, nil
}

func upsertAnimeCatalogStubsWithTx(ctx context.Context, tx *sql.Tx, animeIDs []int) error {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return nil
	}

	rows := make([]string, 0, len(animeIDs))
	args := make([]any, 0, len(animeIDs))
	for index, animeID := range animeIDs {
		rows = append(rows, fmt.Sprintf("($%d)", index+1))
		args = append(args, animeID)
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO anime_catalog (id)
		VALUES %s
		ON CONFLICT (id) DO NOTHING
	`, strings.Join(rows, ", ")), args...)
	return err
}

func upsertAnimeCatalogDetailsBatchWithTx(ctx context.Context, tx *sql.Tx, detailsBatch []AnimeDetailsInfo, syncedAt time.Time) error {
	ctx = ensureContext(ctx)

	if len(detailsBatch) == 0 {
		return nil
	}

	rows := make([]string, 0, len(detailsBatch))
	args := make([]any, 0, len(detailsBatch)*8)
	argIndex := 1
	for _, details := range detailsBatch {
		rows = append(rows, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, NOW())",
			argIndex,
			argIndex+1,
			argIndex+2,
			argIndex+3,
			argIndex+4,
			argIndex+5,
			argIndex+6,
			argIndex+7,
		))
		args = append(
			args,
			details.ID,
			details.Title,
			details.MediaType,
			nullableDate(details.StartDate),
			details.ImageMediumURL,
			details.ImageLargeURL,
			true,
			syncedAt,
		)
		argIndex += 8
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO anime_catalog (
			id,
			title,
			media_type,
			start_date,
			img_small_url,
			img_large_url,
			resolved,
			details_synced_at,
			updated_at
		) VALUES %s
		ON CONFLICT (id) DO UPDATE
		SET
			title = EXCLUDED.title,
			media_type = EXCLUDED.media_type,
			start_date = EXCLUDED.start_date,
			img_small_url = EXCLUDED.img_small_url,
			img_large_url = EXCLUDED.img_large_url,
			resolved = EXCLUDED.resolved,
			details_synced_at = EXCLUDED.details_synced_at,
			updated_at = NOW()
	`, strings.Join(rows, ", ")), args...)
	return err
}

func replaceAnimeRelationsBatchWithTx(ctx context.Context, tx *sql.Tx, detailsBatch []AnimeDetailsInfo) error {
	ctx = ensureContext(ctx)

	sourceIDs := make([]int, 0, len(detailsBatch))
	rows := make([]string, 0)
	args := make([]any, 0)
	argIndex := 1

	for _, details := range detailsBatch {
		if details.ID <= 0 {
			continue
		}
		sourceIDs = append(sourceIDs, details.ID)
		for _, relation := range details.Related {
			if relation.ID <= 0 || relation.ID == details.ID {
				continue
			}
			rows = append(rows, fmt.Sprintf(
				"($%d, $%d, $%d)",
				argIndex,
				argIndex+1,
				argIndex+2,
			))
			args = append(
				args,
				details.ID,
				relation.ID,
				relation.RelationType,
			)
			argIndex += 3
		}
	}

	sourceIDs = uniquePositiveIDs(sourceIDs)
	if len(sourceIDs) == 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM anime_relations
		WHERE id IN (%s)
	`, buildSQLPlaceholders(1, len(sourceIDs))), intsToAnySlice(sourceIDs)...); err != nil {
		return err
	}

	if len(rows) == 0 {
		return nil
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO anime_relations (
			id,
			related_id,
			relation_type
		) VALUES %s
		ON CONFLICT (id, related_id) DO UPDATE
		SET
			relation_type = EXCLUDED.relation_type
	`, strings.Join(rows, ", ")), args...)
	return err
}

func collectAnimeCatalogStubIDs(detailsBatch []AnimeDetailsInfo) []int {
	ids := make([]int, 0, len(detailsBatch))
	for _, details := range detailsBatch {
		if details.ID > 0 {
			ids = append(ids, details.ID)
		}
		ids = append(ids, details.RelatedIDs...)
	}

	return uniquePositiveIDs(ids)
}

func normalizeAnimeCatalogDetailsBatch(detailsBatch []AnimeDetailsInfo) ([]AnimeDetailsInfo, error) {
	if len(detailsBatch) == 0 {
		return nil, nil
	}

	normalizedByID := make(map[int]AnimeDetailsInfo, len(detailsBatch))
	order := make([]int, 0, len(detailsBatch))
	seen := make(map[int]struct{}, len(detailsBatch))
	for _, details := range detailsBatch {
		if details.ID <= 0 {
			return nil, fmt.Errorf("anime catalog details require a positive anime id")
		}

		cloned := cloneAnimeDetailsInfo(details)
		ensureAnimeDetailsRelatedIDs(&cloned)
		if _, ok := seen[cloned.ID]; !ok {
			order = append(order, cloned.ID)
			seen[cloned.ID] = struct{}{}
		}
		normalizedByID[cloned.ID] = cloned
	}

	normalized := make([]AnimeDetailsInfo, 0, len(order))
	for _, animeID := range order {
		normalized = append(normalized, normalizedByID[animeID])
	}
	return normalized, nil
}

func isTraversableAnimeRelationType(relationType string) bool {
	switch strings.ToLower(strings.TrimSpace(relationType)) {
	case "character", "other":
		return false
	default:
		return true
	}
}

func collectTraversableRelatedIDs(details AnimeDetailsInfo) []int {
	normalized := cloneAnimeDetailsInfo(details)
	ensureAnimeDetailsRelatedIDs(&normalized)

	relatedIDs := make([]int, 0, len(normalized.Related))
	for _, relation := range normalized.Related {
		if relation.ID <= 0 || !isTraversableAnimeRelationType(relation.RelationType) {
			continue
		}
		relatedIDs = append(relatedIDs, relation.ID)
	}

	return relatedIDs
}

func formatAnimeRelationType(relationType string) string {
	relationType = strings.TrimSpace(relationType)
	if relationType == "" {
		return ""
	}

	label := strings.ToLower(strings.ReplaceAll(relationType, "_", " "))
	return strings.ToUpper(label[:1]) + label[1:]
}

func ensureAnimeDetailsRelatedIDs(details *AnimeDetailsInfo) {
	if details == nil {
		return
	}

	related := make([]AnimeRelationInfo, 0, len(details.Related)+len(details.RelatedIDs))
	relatedIndexByID := make(map[int]int, len(details.Related)+len(details.RelatedIDs))
	mergeRelated := func(candidate AnimeRelationInfo) {
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
		mergeRelated(AnimeRelationInfo{ID: relatedID})
	}

	relatedIDs := make([]int, 0, len(related))
	for _, relation := range related {
		relatedIDs = append(relatedIDs, relation.ID)
	}

	details.Related = related
	details.RelatedIDs = relatedIDs
}

func cloneAnimeDetailsInfo(details AnimeDetailsInfo) AnimeDetailsInfo {
	details.Related = append([]AnimeRelationInfo(nil), details.Related...)
	details.RelatedIDs = append([]int(nil), details.RelatedIDs...)
	return details
}

func cloneAnimeDetailsInfos(detailsBatch []AnimeDetailsInfo) []AnimeDetailsInfo {
	cloned := make([]AnimeDetailsInfo, 0, len(detailsBatch))
	for _, details := range detailsBatch {
		cloned = append(cloned, cloneAnimeDetailsInfo(details))
	}
	return cloned
}

func nullableDate(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		return parsed.Format("2006-01-02")
	}

	return nil
}

func buildSQLPlaceholders(start, count int) string {
	if count <= 0 {
		return ""
	}

	placeholders := make([]string, 0, count)
	for i := 0; i < count; i++ {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+i))
	}

	return strings.Join(placeholders, ", ")
}

func intsToAnySlice(ids []int) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return args
}

func int64sToAnySlice(ids []int64) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return args
}

func uniquePositiveInt64s(ids []int64) []int64 {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
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
