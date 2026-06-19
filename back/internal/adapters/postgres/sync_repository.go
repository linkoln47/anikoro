package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

type SyncAnimeRepository struct {
	catalog   *CatalogRepository
	userAnime *UserAnimeRepository
	franchise *FranchiseRepository
}

type CatalogRepository struct {
	db *sql.DB
}

type UserAnimeRepository struct {
	db     *sql.DB
	logger ports.SyncLogger
}

type FranchiseRepository struct {
	db     *sql.DB
	logger ports.SyncLogger
}

var _ ports.SyncAnimeRepository = (*SyncAnimeRepository)(nil)
var _ ports.AnimeCatalogRepository = (*CatalogRepository)(nil)
var _ ports.UserAnimeRepository = (*UserAnimeRepository)(nil)
var _ ports.FranchiseRepository = (*FranchiseRepository)(nil)

type animeFranchiseComponent struct {
	MemberIDs []int
	MemberKey string
}

func NewSyncAnimeRepository(db *sql.DB, logger ports.SyncLogger) *SyncAnimeRepository {
	return &SyncAnimeRepository{
		catalog:   NewCatalogRepository(db),
		userAnime: NewUserAnimeRepository(db, logger),
		franchise: NewFranchiseRepository(db, logger),
	}
}

func NewCatalogRepository(db *sql.DB) *CatalogRepository {
	return &CatalogRepository{db: db}
}

func NewUserAnimeRepository(db *sql.DB, logger ports.SyncLogger) *UserAnimeRepository {
	return &UserAnimeRepository{db: db, logger: logger}
}

func NewFranchiseRepository(db *sql.DB, logger ports.SyncLogger) *FranchiseRepository {
	return &FranchiseRepository{db: db, logger: logger}
}

func (repo *SyncAnimeRepository) ClearUserAnimeSnapshot(ctx context.Context, userID int64) error {
	return repo.userAnime.ClearUserAnimeSnapshot(ctx, userID)
}

func (repo *SyncAnimeRepository) UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error {
	return repo.catalog.UpsertAnimeCatalogStubs(ctx, animeIDs)
}

func (repo *SyncAnimeRepository) GetAnimeCatalogState(ctx context.Context, animeID int) (domain.AnimeCatalogState, bool, error) {
	return repo.catalog.GetAnimeCatalogState(ctx, animeID)
}

func (repo *SyncAnimeRepository) GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]domain.AnimeCatalogState, error) {
	return repo.catalog.GetAnimeCatalogStates(ctx, animeIDs)
}

func (repo *SyncAnimeRepository) GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error) {
	return repo.catalog.GetAnimeCatalogMediaType(ctx, animeID)
}

func (repo *SyncAnimeRepository) GetAnimeCatalogSummary(ctx context.Context, animeID int) (domain.AnimeCatalogSummary, bool, error) {
	return repo.catalog.GetAnimeCatalogSummary(ctx, animeID)
}

func (repo *SyncAnimeRepository) ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	return repo.catalog.ListAnimeRelationIDs(ctx, animeID)
}

func (repo *SyncAnimeRepository) ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error) {
	return repo.catalog.ListAnimeRelationIDsBySourceIDs(ctx, animeIDs)
}

func (repo *SyncAnimeRepository) ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	return repo.catalog.ListUndirectedAnimeRelationIDs(ctx, animeID)
}

func (repo *SyncAnimeRepository) SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []domain.AnimeDetails) error {
	return repo.catalog.SaveAnimeCatalogDetailsBatch(ctx, detailsBatch)
}

func (repo *SyncAnimeRepository) RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error {
	return repo.franchise.RefreshAnimeFranchises(ctx, seedIDs)
}

func (repo *SyncAnimeRepository) ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []domain.UserAnimeListEntry) error {
	return repo.userAnime.ReplaceUserAnimeItems(ctx, userID, entries)
}

func (repo *SyncAnimeRepository) UpsertUserAnimeItem(ctx context.Context, userID int64, entry domain.UserAnimeListEntry) error {
	return repo.userAnime.UpsertUserAnimeItem(ctx, userID, entry)
}

func (repo *SyncAnimeRepository) DeleteUserAnimeItem(ctx context.Context, userID int64, animeID int) error {
	return repo.userAnime.DeleteUserAnimeItem(ctx, userID, animeID)
}

func (repo *CatalogRepository) UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error {
	return WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		return upsertAnimeCatalogStubsWithTx(ctx, tx, animeIDs)
	})
}

func (repo *CatalogRepository) GetAnimeCatalogState(ctx context.Context, animeID int) (domain.AnimeCatalogState, bool, error) {
	ctx = ensureContext(ctx)

	var state domain.AnimeCatalogState
	err := repo.db.QueryRowContext(ctx, `
		SELECT id, resolved, COALESCE(details_synced_at, TIMESTAMPTZ 'epoch')
		FROM anime_catalog
		WHERE id = $1
	`, animeID).Scan(&state.AnimeID, &state.Resolved, &state.DetailsSyncedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.AnimeCatalogState{}, false, nil
		}
		return domain.AnimeCatalogState{}, false, err
	}

	return state, true, nil
}

func (repo *CatalogRepository) GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]domain.AnimeCatalogState, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]domain.AnimeCatalogState{}, nil
	}

	args := IntsToAnySlice(animeIDs)
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, resolved, COALESCE(details_synced_at, TIMESTAMPTZ 'epoch')
		FROM anime_catalog
		WHERE id IN (%s)
	`, BuildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[int]domain.AnimeCatalogState, len(animeIDs))
	for rows.Next() {
		var state domain.AnimeCatalogState
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

func (repo *CatalogRepository) GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error) {
	ctx = ensureContext(ctx)

	var mediaType sql.NullString
	err := repo.db.QueryRowContext(ctx, `
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

func (repo *CatalogRepository) GetAnimeCatalogSummary(ctx context.Context, animeID int) (domain.AnimeCatalogSummary, bool, error) {
	ctx = ensureContext(ctx)

	var summary domain.AnimeCatalogSummary
	err := repo.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(title, ''), num_episodes
		FROM anime_catalog
		WHERE id = $1
	`, animeID).Scan(&summary.AnimeID, &summary.Title, &summary.NumEpisodes)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.AnimeCatalogSummary{}, false, nil
		}
		return domain.AnimeCatalogSummary{}, false, err
	}

	return summary, true, nil
}

func (repo *CatalogRepository) ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	ctx = ensureContext(ctx)

	rows, err := repo.db.QueryContext(ctx, `
		SELECT related_id
		FROM anime_relations
		WHERE id = $1
			AND `+TraversableAnimeRelationFilterSQL+`
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

func (repo *CatalogRepository) ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int][]int{}, nil
	}

	args := IntsToAnySlice(animeIDs)
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, related_id
		FROM anime_relations
		WHERE id IN (%s)
			AND %s
		ORDER BY id, related_id
	`, BuildSQLPlaceholders(1, len(animeIDs)), TraversableAnimeRelationFilterSQL), args...)
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

func (repo *CatalogRepository) ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	return listUndirectedAnimeRelationIDsWithContext(ctx, repo.db, animeID)
}

func (repo *CatalogRepository) SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []domain.AnimeDetails) error {
	ctx = ensureContext(ctx)

	normalized, err := normalizeAnimeCatalogDetailsBatch(detailsBatch)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}

	return WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
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

func listCatalogItemsByIDsWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) (map[int]domain.FranchiseEntry, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]domain.FranchiseEntry{}, nil
	}

	args := IntsToAnySlice(animeIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id,
			COALESCE(title, ''),
			COALESCE(media_type, ''),
			COALESCE(start_date::text, ''),
			COALESCE(img_small_url, ''),
			COALESCE(img_large_url, ''),
			num_episodes
		FROM anime_catalog
		WHERE id IN (%s)
	`, BuildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[int]domain.FranchiseEntry, len(animeIDs))
	for rows.Next() {
		var item domain.FranchiseEntry
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
			&item.NumEpisodes,
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

func listRelationsBySourceIDsWithContext(ctx context.Context, tx *sql.Tx, sourceIDs []int) (map[int]map[int]domain.AnimeRelation, error) {
	ctx = ensureContext(ctx)

	sourceIDs = uniquePositiveIDs(sourceIDs)
	if len(sourceIDs) == 0 {
		return map[int]map[int]domain.AnimeRelation{}, nil
	}

	args := IntsToAnySlice(sourceIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			id,
			related_id,
			COALESCE(relation_type, '')
		FROM anime_relations
		WHERE id IN (%s)
	`, BuildSQLPlaceholders(1, len(sourceIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	relationMap := make(map[int]map[int]domain.AnimeRelation, len(sourceIDs))
	for rows.Next() {
		var (
			sourceID int
			relation domain.AnimeRelation
		)
		if err := rows.Scan(&sourceID, &relation.ID, &relation.RelationType); err != nil {
			return nil, err
		}
		relation.RelationTypeFormatted = domain.FormatAnimeRelationType(relation.RelationType)

		targets := relationMap[sourceID]
		if targets == nil {
			targets = make(map[int]domain.AnimeRelation)
			relationMap[sourceID] = targets
		}
		targets[relation.ID] = relation
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return relationMap, nil
}

func listUndirectedAnimeRelationIDsWithContext(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, animeID int) ([]int, error) {
	ctx = ensureContext(ctx)

	rows, err := queryer.QueryContext(ctx, `
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

func upsertAnimeCatalogDetailsBatchWithTx(ctx context.Context, tx *sql.Tx, detailsBatch []domain.AnimeDetails, syncedAt time.Time) error {
	ctx = ensureContext(ctx)

	if len(detailsBatch) == 0 {
		return nil
	}

	rows := make([]string, 0, len(detailsBatch))
	args := make([]any, 0, len(detailsBatch)*11)
	argIndex := 1
	for _, details := range detailsBatch {
		rows = append(rows, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, NOW())",
			argIndex,
			argIndex+1,
			argIndex+2,
			argIndex+3,
			argIndex+4,
			argIndex+5,
			argIndex+6,
			argIndex+7,
			argIndex+8,
			argIndex+9,
			argIndex+10,
		))
		numEpisodes := details.NumEpisodes
		if numEpisodes < 0 {
			numEpisodes = 0
		}
		args = append(
			args,
			details.ID,
			details.Title,
			details.MediaType,
			NullableDate(details.StartDate),
			NullablePositiveInt(details.StartSeasonYear),
			NullableString(details.StartSeasonName),
			details.ImageMediumURL,
			details.ImageLargeURL,
			numEpisodes,
			true,
			syncedAt,
		)
		argIndex += 11
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO anime_catalog (
			id,
			title,
			media_type,
			start_date,
			start_season_year,
			start_season_name,
			img_small_url,
			img_large_url,
			num_episodes,
			resolved,
			details_synced_at,
			updated_at
		) VALUES %s
		ON CONFLICT (id) DO UPDATE
		SET
			title = EXCLUDED.title,
			media_type = EXCLUDED.media_type,
			start_date = EXCLUDED.start_date,
			start_season_year = EXCLUDED.start_season_year,
			start_season_name = EXCLUDED.start_season_name,
			img_small_url = EXCLUDED.img_small_url,
			img_large_url = EXCLUDED.img_large_url,
			num_episodes = EXCLUDED.num_episodes,
			resolved = EXCLUDED.resolved,
			details_synced_at = EXCLUDED.details_synced_at,
			updated_at = NOW()
	`, strings.Join(rows, ", ")), args...)
	return err
}

func replaceAnimeRelationsBatchWithTx(ctx context.Context, tx *sql.Tx, detailsBatch []domain.AnimeDetails) error {
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
			rows = append(rows, fmt.Sprintf("($%d, $%d, $%d)", argIndex, argIndex+1, argIndex+2))
			args = append(args, details.ID, relation.ID, relation.RelationType)
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
	`, BuildSQLPlaceholders(1, len(sourceIDs))), IntsToAnySlice(sourceIDs)...); err != nil {
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

func collectAnimeCatalogStubIDs(detailsBatch []domain.AnimeDetails) []int {
	ids := make([]int, 0, len(detailsBatch))
	for _, details := range detailsBatch {
		if details.ID > 0 {
			ids = append(ids, details.ID)
		}
		ids = append(ids, details.RelatedIDs...)
	}

	return uniquePositiveIDs(ids)
}

func normalizeAnimeCatalogDetailsBatch(detailsBatch []domain.AnimeDetails) ([]domain.AnimeDetails, error) {
	if len(detailsBatch) == 0 {
		return nil, nil
	}

	normalizedByID := make(map[int]domain.AnimeDetails, len(detailsBatch))
	order := make([]int, 0, len(detailsBatch))
	seen := make(map[int]struct{}, len(detailsBatch))
	for _, details := range detailsBatch {
		if details.ID <= 0 {
			return nil, fmt.Errorf("anime catalog details require a positive anime id")
		}

		cloned := domain.CloneAnimeDetails(details)
		domain.EnsureAnimeDetailsRelatedIDs(&cloned)
		if _, ok := seen[cloned.ID]; !ok {
			order = append(order, cloned.ID)
			seen[cloned.ID] = struct{}{}
		}
		normalizedByID[cloned.ID] = cloned
	}

	normalized := make([]domain.AnimeDetails, 0, len(order))
	for _, animeID := range order {
		normalized = append(normalized, normalizedByID[animeID])
	}
	return normalized, nil
}

func (repo *UserAnimeRepository) ClearUserAnimeSnapshot(ctx context.Context, userID int64) error {
	ctx = ensureContext(ctx)

	return WithUserTx(ctx, repo.db, userID, nil, func(tx *sql.Tx) error {
		if repo.logger != nil {
			repo.logger.Info("db", "clearing empty user anime snapshot", "user_id", userID)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items WHERE user_id = $1`, userID); err != nil {
			return err
		}

		return nil
	})
}

func (repo *UserAnimeRepository) ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []domain.UserAnimeListEntry) error {
	ctx = ensureContext(ctx)

	return WithUserTx(ctx, repo.db, userID, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items WHERE user_id = $1`, userID); err != nil {
			return err
		}

		statusIDs, err := animeListStatusIDsByCodeWithContext(ctx, tx)
		if err != nil {
			return err
		}

		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO user_anime_items (
				user_id,
				anime_id,
				list_status_id,
				source_title,
				score,
				watched_episodes,
				synced_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
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
			statusCode := string(entry.ListStatus)
			statusID, ok := statusIDs[statusCode]
			if !ok {
				return fmt.Errorf("unknown anime list status %q for anime %d", statusCode, entry.ID)
			}
			if _, err := stmt.ExecContext(
				ctx,
				userID,
				entry.ID,
				statusID,
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

func (repo *UserAnimeRepository) UpsertUserAnimeItem(ctx context.Context, userID int64, entry domain.UserAnimeListEntry) error {
	ctx = ensureContext(ctx)

	if entry.ID <= 0 {
		return fmt.Errorf("anime id must be positive")
	}

	return WithUserTx(ctx, repo.db, userID, nil, func(tx *sql.Tx) error {
		statusIDs, err := animeListStatusIDsByCodeWithContext(ctx, tx)
		if err != nil {
			return err
		}

		statusCode := string(entry.ListStatus)
		statusID, ok := statusIDs[statusCode]
		if !ok {
			return fmt.Errorf("unknown anime list status %q for anime %d", statusCode, entry.ID)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO user_anime_items (
				user_id,
				anime_id,
				list_status_id,
				source_title,
				score,
				watched_episodes,
				synced_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (user_id, anime_id) DO UPDATE
			SET
				list_status_id = EXCLUDED.list_status_id,
				score = EXCLUDED.score,
				watched_episodes = EXCLUDED.watched_episodes,
				synced_at = EXCLUDED.synced_at
		`, userID, entry.ID, statusID, entry.Title, entry.Score, entry.NumEpisodesWatched, time.Now().UTC())
		return err
	})
}

func (repo *UserAnimeRepository) DeleteUserAnimeItem(ctx context.Context, userID int64, animeID int) error {
	ctx = ensureContext(ctx)

	if animeID <= 0 {
		return fmt.Errorf("anime id must be positive")
	}

	return WithUserTx(ctx, repo.db, userID, nil, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			DELETE FROM user_anime_items
			WHERE user_id = $1 AND anime_id = $2
		`, userID, animeID)
		return err
	})
}

func animeListStatusIDsByCodeWithContext(ctx context.Context, tx *sql.Tx) (map[string]int, error) {
	ctx = ensureContext(ctx)

	rows, err := tx.QueryContext(ctx, `
		SELECT code, id
		FROM anime_list_statuses
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statusIDs := make(map[string]int)
	for rows.Next() {
		var (
			code string
			id   int
		)
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		statusIDs[code] = id
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return statusIDs, nil
}

func (repo *FranchiseRepository) RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error {
	ctx = ensureContext(ctx)

	seedIDs = uniquePositiveIDs(seedIDs)
	if len(seedIDs) == 0 {
		return nil
	}

	return WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		if repo.logger != nil {
			repo.logger.Info("db", "refreshing global anime franchises", "table", AnimeFranchisesTableName, "seed_count", len(seedIDs))
		}

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

			memberIDs, err := repo.collectFranchiseIDsWithContext(ctx, tx, []int{seedID})
			if err != nil {
				return err
			}
			if len(memberIDs) == 0 {
				memberIDs = []int{seedID}
			}
			for _, memberID := range memberIDs {
				coveredAnimeIDs[memberID] = struct{}{}
			}

			memberKey := domain.BuildGroupKey(memberIDs)
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

func (repo *FranchiseRepository) collectFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, seedIDs []int) ([]int, error) {
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
	`, BuildSQLPlaceholders(1, len(animeIDs))), IntsToAnySlice(animeIDs)...)
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

func listAnimeFranchiseMemberIDsByFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, franchiseIDs []int64) (map[int64][]int, error) {
	ctx = ensureContext(ctx)

	franchiseIDs = UniquePositiveInt64s(franchiseIDs)
	if len(franchiseIDs) == 0 {
		return map[int64][]int{}, nil
	}

	args := Int64sToAnySlice(franchiseIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT franchise_id, anime_id
		FROM anime_franchise_members
		WHERE franchise_id IN (%s)
		ORDER BY franchise_id, anime_id
	`, BuildSQLPlaceholders(1, len(franchiseIDs))), args...)
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

func deleteAnimeFranchiseMembersByFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, franchiseIDs []int64) error {
	ctx = ensureContext(ctx)

	franchiseIDs = UniquePositiveInt64s(franchiseIDs)
	if len(franchiseIDs) == 0 {
		return nil
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM anime_franchise_members
		WHERE franchise_id IN (%s)
	`, BuildSQLPlaceholders(1, len(franchiseIDs))), Int64sToAnySlice(franchiseIDs)...)
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
