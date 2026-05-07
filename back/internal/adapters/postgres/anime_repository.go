package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"test/internal/domain"
	"test/internal/ports"
)

type AnimeRepository struct {
	db *sql.DB
}

var _ ports.AnimeReadRepository = (*AnimeRepository)(nil)

func NewAnimeRepository(db *sql.DB) *AnimeRepository {
	return &AnimeRepository{db: db}
}

func (repo *AnimeRepository) ListAnime(ctx context.Context, userID int64) ([]domain.AnimeListItem, error) {
	ctx = ensureContext(ctx)

	anime := make([]domain.AnimeListItem, 0)
	err := WithUserTx(ctx, repo.db, userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entrySnapshots, err := repo.listAnimeEntrySnapshotsWithContext(ctx, tx, userID)
		if err != nil {
			return err
		}

		for _, snapshot := range entrySnapshots {
			item := snapshot.Item
			item.Franchise = []domain.FranchiseEntry{}

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

func (repo *AnimeRepository) GetStats(ctx context.Context, userID int64) (domain.AnimeStats, error) {
	ctx = ensureContext(ctx)

	stats := domain.AnimeStats{}
	err := WithUserTx(ctx, repo.db, userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		entries, err := repo.listAnimeEntrySnapshotsWithContext(ctx, tx, userID)
		if err != nil {
			return err
		}
		stats = domain.CountAnimeListStats(entries)
		return nil
	})
	if err != nil {
		return domain.AnimeStats{}, err
	}

	return stats, nil
}

func (repo *AnimeRepository) listAnimeEntrySnapshotsWithContext(ctx context.Context, tx *sql.Tx, userID int64) ([]domain.AnimeListEntry, error) {
	ctx = ensureContext(ctx)

	rows, err := tx.QueryContext(ctx, `
		SELECT
			ui.anime_id,
			ui.source_title,
			als.code,
			ui.score,
			ui.watched_episodes,
			ui.synced_at,
			COALESCE(ac.title, ''),
			COALESCE(ac.media_type, ''),
			COALESCE(fm.franchise_id, 0),
			COALESCE(fr.representative_anime_id, ui.anime_id),
			COALESCE(frac.title, '')
		FROM user_anime_items ui
		JOIN anime_list_statuses als ON als.id = ui.list_status_id
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

	inputs := make([]domain.AnimeListGroupInput, 0)
	franchiseIDs := make([]int64, 0)

	for rows.Next() {
		var input domain.AnimeListGroupInput
		if err := rows.Scan(
			&input.AnimeID,
			&input.SourceTitle,
			&input.ListStatus,
			&input.Score,
			&input.WatchedEpisodes,
			&input.SyncedAt,
			&input.CatalogTitle,
			&input.MediaType,
			&input.FranchiseID,
			&input.RepresentativeAnimeID,
			&input.FranchiseDisplayTitle,
		); err != nil {
			return nil, fmt.Errorf("scan anime entries row: %w", err)
		}

		inputs = append(inputs, input)
		if input.FranchiseID > 0 {
			franchiseIDs = append(franchiseIDs, input.FranchiseID)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate anime entries rows: %w", err)
	}

	franchiseMemberIDs, err := listAnimeFranchiseMemberIDsByFranchiseIDsWithContext(ctx, tx, franchiseIDs)
	if err != nil {
		return nil, err
	}

	return domain.BuildAnimeListEntries(inputs, franchiseMemberIDs)
}

func (repo *AnimeRepository) buildFranchiseItemsWithContext(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	groupMemberIDs []int,
	franchiseIDs []int,
) ([]domain.FranchiseEntry, error) {
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

	return domain.BuildFranchiseEntries(catalogItems, userStates, relationMap, groupMemberIDs, franchiseIDs), nil
}

func listUserAnimeItemsByIDsWithContext(ctx context.Context, tx *sql.Tx, userID int64, animeIDs []int) (map[int]domain.AnimeUserListState, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]domain.AnimeUserListState{}, nil
	}

	args := make([]any, 0, len(animeIDs)+1)
	args = append(args, userID)
	args = append(args, IntsToAnySlice(animeIDs)...)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT ui.anime_id, COALESCE(ui.score, 0), ui.watched_episodes, als.code
		FROM user_anime_items ui
		JOIN anime_list_statuses als ON als.id = ui.list_status_id
		WHERE ui.user_id = $1
			AND ui.anime_id IN (%s)
	`, BuildSQLPlaceholders(2, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[int]domain.AnimeUserListState, len(animeIDs))
	for rows.Next() {
		var (
			animeID int
			state   domain.AnimeUserListState
		)
		if err := rows.Scan(&animeID, &state.Score, &state.WatchedEpisodes, &state.ListStatus); err != nil {
			return nil, err
		}
		items[animeID] = state
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
