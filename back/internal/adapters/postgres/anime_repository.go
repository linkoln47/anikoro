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
			num_episodes,
			mal_score
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
		var (
			item  domain.SeasonalAnimeItem
			score sql.NullFloat64
		)
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
			&item.NumEpisodes,
			&score,
		); err != nil {
			return nil, fmt.Errorf("scan season anime row: %w", err)
		}
		if score.Valid {
			item.MeanScore = &score.Float64
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate season anime rows: %w", err)
	}

	animeIDs := make([]int, 0, len(items))
	for _, item := range items {
		animeIDs = append(animeIDs, item.ID)
	}
	genresByAnime, err := listGenresByAnimeIDsWithContext(ctx, repo.db, animeIDs)
	if err != nil {
		return nil, fmt.Errorf("load season anime genres: %w", err)
	}
	for index := range items {
		items[index].Genres = genresByAnime[items[index].ID]
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

		genresByAnime, err := listGenresByAnimeIDsWithContext(ctx, tx, memberIDs)
		if err != nil {
			return err
		}
		item.Genres = domain.AggregateFranchiseGenres(genresByAnime, memberIDs)

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

	var groupID int64
	err := tx.QueryRowContext(ctx, `
		SELECT group_id
		FROM anime_franchises
		WHERE anime_id = $1
	`, animeID).Scan(&groupID)
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
		FROM anime_franchises
		WHERE group_id = $1
		ORDER BY anime_id
	`, groupID)
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

// ListFranchises returns a page of franchise groups from the catalog, the same
// way the dashboard groups a user's anime into franchises but scoped to the whole
// catalog instead of one user. Each group is reduced to its representative (the
// smallest member id, matching both the dashboard and the single franchise
// view), and the count carries however many titles it bundles — a franchise may
// hold a single title (a standalone film) or many. Groups whose representative
// has no title yet (an unresolved stub) are skipped to keep the grid clean.
//
// The query filters by the representative's media type and title and windows the
// result with Limit/Offset so the grid loads one page at a time instead of the
// whole catalog. The returned int is the total number of groups matching the
// filters (before the window), for the caller's paging UI. Like the seasonal
// listing it reads only the global catalog and is not scoped to a user.
func (repo *AnimeRepository) ListFranchises(ctx context.Context, query domain.FranchiseQuery) ([]domain.FranchiseSummary, int, error) {
	ctx = ensureContext(ctx)

	// franchise_score is the average MAL community score over the members that
	// have one (AVG ignores NULLs); it is NULL when no member is scored. Groups
	// are ordered by that rating first, so the highest-rated franchises lead the
	// "all anime" grid, with unrated groups sorted last by title. The order is
	// deterministic (rep_id breaks ties), so Limit/Offset paging is stable.
	//
	// The optional filters short-circuit when their parameter is empty, so the
	// same statement serves the unfiltered grid. COUNT(*) OVER() carries the total
	// match count alongside each windowed row.
	rows, err := repo.db.QueryContext(ctx, `
		WITH groups AS (
			SELECT
				af.group_id AS rep_id,
				COUNT(*) AS member_count,
				AVG(ac.mal_score) AS franchise_score
			FROM anime_franchises af
			JOIN anime_catalog ac ON ac.id = af.anime_id
			GROUP BY af.group_id
		),
		filtered AS (
			SELECT
				g.rep_id,
				rep.title,
				rep.media_type,
				rep.start_date,
				rep.img_small_url,
				rep.img_large_url,
				rep.num_episodes,
				g.member_count,
				g.franchise_score
			FROM groups g
			JOIN anime_catalog rep ON rep.id = g.rep_id
			WHERE COALESCE(rep.title, '') <> ''
				AND ($1 = '' OR rep.media_type = $1)
				AND ($2 = '' OR rep.title ILIKE '%' || $2 || '%')
		)
		SELECT
			rep_id,
			COALESCE(title, ''),
			COALESCE(media_type, ''),
			COALESCE(start_date::text, ''),
			COALESCE(img_small_url, ''),
			COALESCE(img_large_url, ''),
			num_episodes,
			member_count,
			franchise_score,
			COUNT(*) OVER() AS total
		FROM filtered
		ORDER BY franchise_score DESC NULLS LAST, COALESCE(NULLIF(title, ''), '~') ASC, rep_id ASC
		LIMIT $3 OFFSET $4
	`, query.MediaType, query.Search, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query franchises: %w", err)
	}
	defer rows.Close()

	items := make([]domain.FranchiseSummary, 0)
	total := 0
	for rows.Next() {
		var (
			item  domain.FranchiseSummary
			score sql.NullFloat64
		)
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
			&item.NumEpisodes,
			&item.MemberCount,
			&score,
			&total,
		); err != nil {
			return nil, 0, fmt.Errorf("scan franchise row: %w", err)
		}
		if score.Valid {
			rounded := domain.RoundScore(score.Float64)
			item.Score = &rounded
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate franchise rows: %w", err)
	}

	return items, total, nil
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

	// group_id is the franchise's representative (smallest member id), so it
	// serves as both the franchise identifier and the representative anime id —
	// no separate MIN() lookup is needed. frac is the representative's catalog row.
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
			COALESCE(af.group_id, 0),
			COALESCE(af.group_id, ui.anime_id),
			COALESCE(frac.title, ''),
			COALESCE(ac.resolved, false)
		FROM user_anime_items ui
		JOIN anime_list_statuses als ON als.id = ui.list_status_id
		LEFT JOIN anime_catalog ac ON ac.id = ui.anime_id
		LEFT JOIN anime_franchises af ON af.anime_id = ui.anime_id
		LEFT JOIN anime_catalog frac ON frac.id = af.group_id
		WHERE ui.user_id = $1
		ORDER BY COALESCE(af.group_id, 0), ui.anime_id
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query anime entries: %w", err)
	}
	defer rows.Close()

	inputs := make([]domain.AnimeListGroupInput, 0)
	groupIDs := make([]int64, 0)

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
			&input.Resolved,
		); err != nil {
			return nil, fmt.Errorf("scan anime entries row: %w", err)
		}

		inputs = append(inputs, input)
		if input.FranchiseID > 0 {
			groupIDs = append(groupIDs, input.FranchiseID)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate anime entries rows: %w", err)
	}

	franchiseMemberIDs, err := listAnimeFranchiseMemberIDsByGroupIDsWithContext(ctx, tx, groupIDs)
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

// listGenresByAnimeIDsWithContext returns each requested anime's genres, keyed
// by anime id and sorted by name. It backs both the seasonal genre filter
// (per-anime genres) and the single franchise view (which unions the per-anime
// genres of the franchise members). It reads only the global genres tables, so
// it works through any queryer (the read-only catalog db or a transaction).
func listGenresByAnimeIDsWithContext(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, animeIDs []int) (map[int][]domain.AnimeGenre, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int][]domain.AnimeGenre{}, nil
	}

	args := IntsToAnySlice(animeIDs)
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`
		SELECT ag.anime_id, g.id, g.name
		FROM anime_genres ag
		JOIN genres g ON g.id = ag.genre_id
		WHERE ag.anime_id IN (%s)
		ORDER BY ag.anime_id, g.name, g.id
	`, BuildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int][]domain.AnimeGenre, len(animeIDs))
	for rows.Next() {
		var (
			animeID int
			genre   domain.AnimeGenre
		)
		if err := rows.Scan(&animeID, &genre.ID, &genre.Name); err != nil {
			return nil, err
		}
		result[animeID] = append(result[animeID], genre)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
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
