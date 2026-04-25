package main

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"test/internal/domain"
)

type PostgresAnimeRepository struct {
	db *sql.DB
}

type animeListEntrySnapshot struct {
	Item               AnimeListItem
	GroupMemberIDs     []int
	FranchiseMemberIDs []int
}

type userAnimeItemState struct {
	Score           int
	WatchedEpisodes int
}

func newPostgresAnimeRepository(db *sql.DB) *PostgresAnimeRepository {
	return &PostgresAnimeRepository{db: db}
}

func (repo *PostgresAnimeRepository) ListAnime(ctx context.Context, userID int64) ([]AnimeListItem, error) {
	ctx = ensureContext(ctx)

	anime := make([]AnimeListItem, 0)
	err := withUserTx(ctx, repo.db, userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entrySnapshots, err := repo.listAnimeEntrySnapshotsWithContext(ctx, tx, userID)
		if err != nil {
			return err
		}

		for _, snapshot := range entrySnapshots {
			item := snapshot.Item
			item.Franchise = []FranchiseEntry{}

			if len(snapshot.FranchiseMemberIDs) > 0 {
				item.Franchise, err = repo.buildFranchiseItemsWithContext(
					ctx,
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

func (repo *PostgresAnimeRepository) GetStats(ctx context.Context, userID int64) (AnimeStats, error) {
	ctx = ensureContext(ctx)

	var stats AnimeStats
	err := withUserTx(ctx, repo.db, userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entries, err := repo.listAnimeEntrySnapshotsWithContext(ctx, tx, userID)
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
		return AnimeStats{}, err
	}

	stats.TotalCount = stats.SeriesCount + stats.MoviesCount
	return stats, nil
}

func (repo *PostgresAnimeRepository) listAnimeEntrySnapshotsWithContext(ctx context.Context, tx *sql.Tx, userID int64) ([]animeListEntrySnapshot, error) {
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
		memberIDs, err := domain.SortedMemberIDs(group.memberIDs)
		if err != nil {
			return nil, err
		}

		avgScore := 0.0
		if group.scoredItemsCount > 0 {
			avgScore = domain.RoundScore(float64(group.totalScore) / float64(group.scoredItemsCount))
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
			Item: AnimeListItem{
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

func (repo *PostgresAnimeRepository) buildFranchiseItemsWithContext(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	groupMemberIDs []int,
	franchiseIDs []int,
) ([]FranchiseEntry, error) {
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
				relation.RelationTypeFormatted = domain.FormatAnimeRelationType(relation.RelationType)
			}
			return relation.RelationType, relation.RelationTypeFormatted, true
		}
	}

	return "", "", false
}
