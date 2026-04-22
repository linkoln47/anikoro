package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	userAnimeGroupsTableName          = "user_anime_groups"
	usersTableName                    = "users"
	malTokensTable                    = "mal_tokens"
	userScopeSetting                  = "app.user_id"
	defaultDBTimeout                  = 5 * time.Second
	defaultMaxOpenDB                  = 10
	defaultMaxIdleDB                  = 5
	traversableAnimeRelationFilterSQL = "COALESCE(LOWER(relation_type), '') NOT IN ('character', 'other')"
)

type User struct {
	ID       int64
	Username string
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

type groupedAnimeEntry struct {
	ID                 int
	Type               string
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
	Item           AnimeItem
	GroupMemberIDs []int
}

type userAnimeItemState struct {
	Score           int
	WatchedEpisodes int
}

func (a *App) ListAnime(userID int64) ([]AnimeItem, error) {
	anime := make([]AnimeItem, 0)

	err := a.withUserTx(context.Background(), userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entrySnapshots, err := a.listAnimeEntrySnapshotsWithContext(context.Background(), tx)
		if err != nil {
			return err
		}

		for _, snapshot := range entrySnapshots {
			item := snapshot.Item
			item.Franchise = []FranchiseItem{}

			if len(snapshot.GroupMemberIDs) > 0 {
				franchiseIDs, err := a.collectFranchiseIDsWithContext(context.Background(), tx, snapshot.GroupMemberIDs)
				if err != nil {
					return fmt.Errorf("collect franchise ids for anime %d: %w", item.ID, err)
				}

				item.Franchise, err = a.buildFranchiseItemsWithContext(
					context.Background(),
					tx,
					snapshot.GroupMemberIDs,
					franchiseIDs,
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

func (a *App) listAnimeEntrySnapshotsWithContext(ctx context.Context, tx *sql.Tx) ([]animeListEntrySnapshot, error) {
	ctx = ensureContext(ctx)

	rows, err := tx.QueryContext(ctx, `
		SELECT
			anime_id,
			anime_type,
			display_title,
			merged_titles,
			avg_score,
			group_member_ids::text,
			watched_episodes_sum,
			synced_at
		FROM user_anime_groups
		ORDER BY CASE anime_type WHEN 'series' THEN 0 ELSE 1 END, anime_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query anime entries: %w", err)
	}
	defer rows.Close()

	entries := make([]animeListEntrySnapshot, 0)
	for rows.Next() {
		var (
			entry          animeListEntrySnapshot
			groupMemberIDs string
			syncedAt       time.Time
		)
		if err := rows.Scan(
			&entry.Item.ID,
			&entry.Item.Type,
			&entry.Item.DisplayTitle,
			&entry.Item.MergedTitles,
			&entry.Item.AvgScore,
			&groupMemberIDs,
			&entry.Item.WatchedEpisodesSum,
			&syncedAt,
		); err != nil {
			return nil, fmt.Errorf("scan anime entries row: %w", err)
		}

		memberIDs, err := parseIntArrayLiteral(groupMemberIDs)
		if err != nil {
			return nil, fmt.Errorf("parse group_member_ids for anime %d: %w", entry.Item.ID, err)
		}

		entry.Item.SyncedAt = syncedAt.UTC().Format(time.RFC3339)
		entry.GroupMemberIDs = memberIDs
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate anime entries rows: %w", err)
	}

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

	userStates, err := listUserAnimeItemsByIDsWithContext(ctx, tx, franchiseIDs)
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

func listUserAnimeItemsByIDsWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) (map[int]userAnimeItemState, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]userAnimeItemState{}, nil
	}

	args := intsToAnySlice(animeIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT anime_id, COALESCE(score, 0), watched_episodes
		FROM user_anime_items
		WHERE anime_id IN (%s)
	`, buildSQLPlaceholders(1, len(animeIDs))), args...)
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
		return tx.QueryRow(`
			SELECT
				COUNT(*) FILTER (WHERE anime_type = 'series'),
				COUNT(*) FILTER (WHERE anime_type = 'movie')
			FROM user_anime_groups
		`).Scan(&stats.SeriesCount, &stats.MoviesCount)
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

func (a *App) SaveGroupedLists(userID int64, seriesGroups, movieGroups []GroupedView) error {
	return a.saveGroupedListsWithContext(context.Background(), userID, seriesGroups, movieGroups)
}

func (a *App) saveGroupedListsWithContext(ctx context.Context, userID int64, seriesGroups, movieGroups []GroupedView) error {
	return a.saveGroupedEntriesWithContext(ctx, userID, flattenGroupedEntries(seriesGroups, movieGroups))
}

func flattenGroupedEntries(seriesGroups, movieGroups []GroupedView) []groupedAnimeEntry {
	entries := make([]groupedAnimeEntry, 0, len(seriesGroups)+len(movieGroups))
	for _, g := range seriesGroups {
		entries = append(entries, groupedAnimeEntry{
			ID:                 g.ID,
			Type:               "series",
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       g.MergedTitles,
			AvgScore:           g.AvgScore,
			GroupMemberIDs:     append([]int(nil), g.GroupMemberIDs...),
			WatchedEpisodesSum: g.WatchedEpisodesSum,
		})
	}
	for _, g := range movieGroups {
		entries = append(entries, groupedAnimeEntry{
			ID:                 g.ID,
			Type:               "movie",
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       g.MergedTitles,
			AvgScore:           g.AvgScore,
			GroupMemberIDs:     append([]int(nil), g.GroupMemberIDs...),
			WatchedEpisodesSum: g.WatchedEpisodesSum,
		})
	}
	return entries
}

func (a *App) saveGroupedEntries(userID int64, entries []groupedAnimeEntry) error {
	return a.saveGroupedEntriesWithContext(context.Background(), userID, entries)
}

func (a *App) saveGroupedEntriesWithContext(ctx context.Context, userID int64, entries []groupedAnimeEntry) error {
	return a.withUserTx(ctx, userID, nil, func(tx *sql.Tx) error {
		return a.replaceEntriesWithContext(ctx, tx, userID, entries)
	})
}

func (a *App) replaceEntries(tx *sql.Tx, userID int64, entries []groupedAnimeEntry) error {
	return a.replaceEntriesWithContext(context.Background(), tx, userID, entries)
}

func (a *App) replaceEntriesWithContext(ctx context.Context, tx *sql.Tx, userID int64, entries []groupedAnimeEntry) error {
	ctx = ensureContext(ctx)

	a.logInfo("db", "rewriting user snapshot in DB table", "table", userAnimeGroupsTableName, "user_id", userID, "rows", len(entries))

	if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_groups`); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO user_anime_groups (
			anime_id,
			anime_type,
			display_title,
			merged_titles,
			avg_score,
			group_member_ids,
			watched_episodes_sum,
			synced_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	syncedAt := time.Now().UTC()
	for _, entry := range entries {
		if _, err := stmt.ExecContext(
			ctx,
			entry.ID,
			entry.Type,
			entry.DisplayTitle,
			entry.MergedTitles,
			entry.AvgScore,
			intArrayLiteral(entry.GroupMemberIDs),
			entry.WatchedEpisodesSum,
			syncedAt,
		); err != nil {
			return err
		}
	}
	return nil
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

func (a *App) replaceUserAnimeItemsWithContext(ctx context.Context, userID int64, entries []AnimeEntry) error {
	ctx = ensureContext(ctx)

	return a.withUserTx(ctx, userID, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items`); err != nil {
			return err
		}

		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO user_anime_items (
				anime_id,
				source_title,
				score,
				watched_episodes,
				synced_at
			) VALUES ($1, $2, $3, $4, $5)
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

		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_groups`); err != nil {
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

func intArrayLiteral(ids []int) string {
	if len(ids) == 0 {
		return "{}"
	}

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}

	return "{" + strings.Join(parts, ",") + "}"
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

func parseIntArrayLiteral(value string) ([]int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "{}" {
		return []int{}, nil
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, fmt.Errorf("invalid array literal %q", value)
	}

	body := strings.TrimSuffix(strings.TrimPrefix(trimmed, "{"), "}")
	if strings.TrimSpace(body) == "" {
		return []int{}, nil
	}

	parts := strings.Split(body, ",")
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("parse array item %q: %w", part, err)
		}
		ids = append(ids, id)
	}

	return ids, nil
}
