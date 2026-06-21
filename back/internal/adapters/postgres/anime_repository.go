package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"test/internal/domain"
	"test/internal/ports"
)

type AnimeRepository struct {
	db *sql.DB
}

var (
	_ ports.AnimeReadRepository  = (*AnimeRepository)(nil)
	_ ports.SeasonReadRepository = (*AnimeRepository)(nil)
)

func NewAnimeRepository(db *sql.DB) *AnimeRepository {
	return &AnimeRepository{db: db}
}

// ListSeasonAnime returns catalog entries that premiered in the given MAL
// season, ordered by title for a stable default the frontend can re-sort.
func (repo *AnimeRepository) ListSeasonAnime(ctx context.Context, season domain.Season) ([]domain.SeasonalAnimeItem, error) {
	ctx = ensureContext(ctx)

	rows, err := repo.db.QueryContext(ctx, `
		SELECT
			id,
			COALESCE(title, ''),
			COALESCE(media_type, ''),
			COALESCE(start_date::text, ''),
			COALESCE(img_small_url, ''),
			COALESCE(img_large_url, ''),
			num_episodes
		FROM anime_catalog
		WHERE start_season_year = $1
			AND start_season_name = $2
		ORDER BY COALESCE(NULLIF(title, ''), '~') ASC, id ASC
	`, season.Year, string(season.Name))
	if err != nil {
		return nil, fmt.Errorf("query season anime: %w", err)
	}
	defer rows.Close()

	items := make([]domain.SeasonalAnimeItem, 0)
	for rows.Next() {
		var item domain.SeasonalAnimeItem
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
			&item.NumEpisodes,
		); err != nil {
			return nil, fmt.Errorf("scan season anime row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate season anime rows: %w", err)
	}

	return items, nil
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
					item.ID,
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

// GetFranchise resolves the global franchise group for a single anime id and
// builds a grouped entry. When userID is positive the caller's list marks are
// decorated onto the franchise entries (read under their RLS scope); userID 0
// yields the same grouping with the user-only fields zeroed. It returns false
// when the anime id is not present in the catalog.
func (repo *AnimeRepository) GetFranchise(ctx context.Context, animeID int, userID int64) (domain.AnimeListItem, bool, error) {
	ctx = ensureContext(ctx)

	if animeID <= 0 {
		return domain.AnimeListItem{}, false, nil
	}

	var (
		item  domain.AnimeListItem
		found bool
	)
	build := func(tx *sql.Tx) error {
		memberIDs, representativeID, ok, err := resolveFranchiseMembersWithContext(ctx, tx, animeID)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		catalogItems, err := listCatalogItemsByIDsWithContext(ctx, tx, memberIDs)
		if err != nil {
			return err
		}

		relationMap, err := listRelationsBySourceIDsWithContext(ctx, tx, memberIDs)
		if err != nil {
			return err
		}

		userStates := map[int]domain.AnimeUserListState{}
		if userID > 0 {
			userStates, err = listUserAnimeItemsByIDsWithContext(ctx, tx, userID, memberIDs)
			if err != nil {
				return err
			}
		}

		franchise := domain.BuildFranchiseEntries(
			catalogItems,
			userStates,
			relationMap,
			memberIDs,
			memberIDs,
			representativeID,
		)
		item = domain.BuildFranchiseItem(representativeID, memberIDs, catalogItems, franchise)
		found = true
		return nil
	}

	// A signed-in caller reads user_anime_items, which is guarded by row-level
	// security, so resolve the franchise inside that user's transaction scope.
	// Anonymous callers only touch the global catalog tables.
	var err error
	if userID > 0 {
		err = WithUserTx(ctx, repo.db, userID, &sql.TxOptions{ReadOnly: true}, build)
	} else {
		err = WithTx(ctx, repo.db, &sql.TxOptions{ReadOnly: true}, build)
	}
	if err != nil {
		return domain.AnimeListItem{}, false, err
	}

	return item, found, nil
}

// resolveFranchiseMembersWithContext returns the sorted member ids of the
// franchise that contains animeID and the representative id (the smallest
// member id). Anime without a franchise row resolve to a single-member group as
// long as they exist in the catalog. The boolean reports catalog presence.
func resolveFranchiseMembersWithContext(ctx context.Context, tx *sql.Tx, animeID int) ([]int, int, bool, error) {
	ctx = ensureContext(ctx)

	var franchiseID int64
	err := tx.QueryRowContext(ctx, `
		SELECT franchise_id
		FROM anime_franchise_members
		WHERE anime_id = $1
	`, animeID).Scan(&franchiseID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No franchise grouping: treat the anime as a standalone group if it is
		// known to the catalog.
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM anime_catalog WHERE id = $1)
		`, animeID).Scan(&exists); err != nil {
			return nil, 0, false, err
		}
		if !exists {
			return nil, 0, false, nil
		}
		return []int{animeID}, animeID, true, nil
	case err != nil:
		return nil, 0, false, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT anime_id
		FROM anime_franchise_members
		WHERE franchise_id = $1
		ORDER BY anime_id
	`, franchiseID)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()

	memberIDs := make([]int, 0)
	for rows.Next() {
		var memberID int
		if err := rows.Scan(&memberID); err != nil {
			return nil, 0, false, err
		}
		memberIDs = append(memberIDs, memberID)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, false, err
	}
	if len(memberIDs) == 0 {
		return []int{animeID}, animeID, true, nil
	}

	return memberIDs, memberIDs[0], true, nil
}

// ListFranchises returns every franchise group in the catalog, the same way the
// dashboard groups a user's anime into franchises but scoped to the whole
// catalog instead of one user. Each group is reduced to its representative (the
// smallest member id, matching both the dashboard and the single franchise
// view), and the count carries however many titles it bundles — a franchise may
// hold a single title (a standalone film) or many. Groups whose representative
// has no title yet (an unresolved stub) are skipped to keep the grid clean. Like
// the seasonal listing it reads only the global catalog and is not scoped to a
// user.
func (repo *AnimeRepository) ListFranchises(ctx context.Context) ([]domain.FranchiseSummary, error) {
	ctx = ensureContext(ctx)

	rows, err := repo.db.QueryContext(ctx, `
		WITH groups AS (
			SELECT MIN(anime_id) AS rep_id, COUNT(*) AS member_count
			FROM anime_franchise_members
			GROUP BY franchise_id
		)
		SELECT
			g.rep_id,
			COALESCE(ac.title, ''),
			COALESCE(ac.media_type, ''),
			COALESCE(ac.start_date::text, ''),
			COALESCE(ac.img_small_url, ''),
			COALESCE(ac.img_large_url, ''),
			ac.num_episodes,
			g.member_count
		FROM groups g
		JOIN anime_catalog ac ON ac.id = g.rep_id
		WHERE COALESCE(ac.title, '') <> ''
		ORDER BY COALESCE(NULLIF(ac.title, ''), '~') ASC, g.rep_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query franchises: %w", err)
	}
	defer rows.Close()

	items := make([]domain.FranchiseSummary, 0)
	for rows.Next() {
		var item domain.FranchiseSummary
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
			&item.NumEpisodes,
			&item.MemberCount,
		); err != nil {
			return nil, fmt.Errorf("scan franchise row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate franchise rows: %w", err)
	}

	return items, nil
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
	primaryID int,
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

	return domain.BuildFranchiseEntries(catalogItems, userStates, relationMap, groupMemberIDs, franchiseIDs, primaryID), nil
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
