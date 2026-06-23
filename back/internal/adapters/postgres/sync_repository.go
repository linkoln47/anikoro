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
	// GroupID is the franchise's representative: the smallest member id. It is
	// MemberIDs[0] because MemberIDs is sorted ascending.
	GroupID int
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

func (repo *SyncAnimeRepository) ListUngroupedResolvedCatalogIDs(ctx context.Context, limit int) ([]int, error) {
	return repo.catalog.ListUngroupedResolvedCatalogIDs(ctx, limit)
}

func (repo *SyncAnimeRepository) RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error {
	return repo.franchise.RefreshAnimeFranchises(ctx, seedIDs)
}

// SaveAnimeCatalogDetailsWithFranchises persists anime details and rebuilds
// the affected franchise groups in a single transaction. Either both succeed
// or neither does, so a resolved=true entry always has consistent franchise rows.
func (repo *SyncAnimeRepository) SaveAnimeCatalogDetailsWithFranchises(ctx context.Context, detailsBatch []domain.AnimeDetails) error {
	ctx = ensureContext(ctx)

	normalized, err := normalizeAnimeCatalogDetailsBatch(detailsBatch)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}

	return WithTx(ctx, repo.catalog.db, nil, func(tx *sql.Tx) error {
		if err := upsertAnimeCatalogStubsWithTx(ctx, tx, collectAnimeCatalogStubIDs(normalized)); err != nil {
			return err
		}
		syncedAt := time.Now().UTC()
		if err := upsertAnimeCatalogDetailsBatchWithTx(ctx, tx, normalized, syncedAt); err != nil {
			return err
		}
		if err := replaceAnimeRelationsBatchWithTx(ctx, tx, normalized); err != nil {
			return err
		}

		savedIDs := make([]int, 0, len(normalized))
		for _, d := range normalized {
			savedIDs = append(savedIDs, d.ID)
		}
		return refreshAnimeFranchisesInTx(ctx, tx, savedIDs, repo.franchise.logger)
	})
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

// ListStaleCatalogIDs returns ids of resolved catalog entries whose details were
// last synced before `before` (a never-synced row counts as stale), oldest
// first, capped at limit. The catalog refresh job uses it to re-hydrate entries
// — and their mal_score — that no recent user sync has touched. A non-positive
// limit returns no ids.
func (repo *CatalogRepository) ListStaleCatalogIDs(ctx context.Context, before time.Time, limit int) ([]int, error) {
	ctx = ensureContext(ctx)

	if limit <= 0 {
		return nil, nil
	}

	rows, err := repo.db.QueryContext(ctx, `
		SELECT id
		FROM anime_catalog
		WHERE resolved = TRUE
			AND (details_synced_at IS NULL OR details_synced_at < $1)
		ORDER BY details_synced_at ASC NULLS FIRST, id ASC
		LIMIT $2
	`, before.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query stale catalog ids: %w", err)
	}
	defer rows.Close()

	ids := make([]int, 0, limit)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale catalog id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale catalog ids: %w", err)
	}

	return ids, nil
}

// ListUnresolvedCatalogIDs returns ids of catalog stubs that have never been
// hydrated (resolved = false), smallest id first, capped at limit. The
// lightweight user sync upserts these stubs without fetching their details; the
// lazy worker drains this queue to hydrate them (and their franchise neighbours)
// from MAL. A non-positive limit returns no ids.
func (repo *CatalogRepository) ListUnresolvedCatalogIDs(ctx context.Context, limit int) ([]int, error) {
	ctx = ensureContext(ctx)

	if limit <= 0 {
		return nil, nil
	}

	rows, err := repo.db.QueryContext(ctx, `
		SELECT id
		FROM anime_catalog
		WHERE resolved = false
		ORDER BY id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query unresolved catalog ids: %w", err)
	}
	defer rows.Close()

	ids := make([]int, 0, limit)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan unresolved catalog id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unresolved catalog ids: %w", err)
	}

	return ids, nil
}

func (repo *CatalogRepository) ListUngroupedResolvedCatalogIDs(ctx context.Context, limit int) ([]int, error) {
	ctx = ensureContext(ctx)

	if limit <= 0 {
		return nil, nil
	}

	rows, err := repo.db.QueryContext(ctx, `
		SELECT id
		FROM anime_catalog c
		WHERE c.resolved = true
			AND NOT EXISTS (
				SELECT 1 FROM anime_franchises f WHERE f.anime_id = c.id
			)
		ORDER BY id ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query ungrouped resolved catalog ids: %w", err)
	}
	defer rows.Close()

	ids := make([]int, 0, limit)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan ungrouped resolved catalog id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ungrouped resolved catalog ids: %w", err)
	}

	return ids, nil
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
			num_episodes,
			mal_score
		FROM anime_catalog
		WHERE id IN (%s)
	`, BuildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make(map[int]domain.FranchiseEntry, len(animeIDs))
	for rows.Next() {
		var (
			item     domain.FranchiseEntry
			malScore sql.NullFloat64
		)
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.MediaType,
			&item.StartDate,
			&item.ImageMediumURL,
			&item.ImageLargeURL,
			&item.NumEpisodes,
			&malScore,
		); err != nil {
			return nil, err
		}
		if malScore.Valid {
			v := malScore.Float64
			item.MalScore = &v
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

// listUndirectedAnimeRelationIDsBatchWithContext is the batch counterpart of
// listUndirectedAnimeRelationIDsWithContext. It fetches undirected neighbours
// for every source id in a single query instead of one query per node, reducing
// BFS traversal from O(nodes) to O(depth) round-trips.
func listUndirectedAnimeRelationIDsBatchWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) (map[int][]int, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int][]int{}, nil
	}

	args := append(IntsToAnySlice(animeIDs), IntsToAnySlice(animeIDs)...)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT source_id, neighbor_id
		FROM (
			SELECT id         AS source_id,
			       related_id AS neighbor_id,
			       COALESCE(LOWER(relation_type), '') AS relation_type
			FROM anime_relations
			WHERE id IN (%s)
			UNION ALL
			SELECT related_id AS source_id,
			       id         AS neighbor_id,
			       COALESCE(LOWER(relation_type), '') AS relation_type
			FROM anime_relations
			WHERE related_id IN (%s)
		) AS neighbors
		WHERE neighbor_id <> source_id
		  AND relation_type NOT IN ('character', 'other')
		ORDER BY source_id, neighbor_id
	`, BuildSQLPlaceholders(1, len(animeIDs)), BuildSQLPlaceholders(len(animeIDs)+1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int][]int, len(animeIDs))
	for rows.Next() {
		var sourceID, neighborID int
		if err := rows.Scan(&sourceID, &neighborID); err != nil {
			return nil, err
		}
		result[sourceID] = append(result[sourceID], neighborID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
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
	args := make([]any, 0, len(detailsBatch)*12)
	argIndex := 1
	for _, details := range detailsBatch {
		rows = append(rows, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
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
			argIndex+11,
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
			NullableScore(details.MalScore),
			true,
			syncedAt,
		)
		argIndex += 12
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
			mal_score,
			resolved,
			details_synced_at
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
			mal_score = EXCLUDED.mal_score,
			resolved = EXCLUDED.resolved,
			details_synced_at = EXCLUDED.details_synced_at
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
		return refreshAnimeFranchisesInTx(ctx, tx, seedIDs, repo.logger)
	})
}

// refreshAnimeFranchisesInTx is the transactionless inner logic of
// RefreshAnimeFranchises. It is called both from RefreshAnimeFranchises (which
// opens its own transaction) and from SaveAnimeCatalogDetailsWithFranchises
// (which reuses the same transaction as the detail write, making the two atomic).
func refreshAnimeFranchisesInTx(ctx context.Context, tx *sql.Tx, seedIDs []int, logger ports.SyncLogger) error {
	seedIDs = uniquePositiveIDs(seedIDs)
	if len(seedIDs) == 0 {
		return nil
	}

	if logger != nil {
		logger.Info("db", "refreshing global anime franchises", "table", AnimeFranchisesTableName, "seed_count", len(seedIDs))
	}

	// Re-evaluate every anime that currently shares a group with a seed, so a
	// franchise that is splitting has all of its old members reconsidered.
	worklist := append([]int(nil), seedIDs...)
	impactedGroupIDs, err := listAnimeFranchiseGroupIDsByAnimeIDsWithContext(ctx, tx, worklist)
	if err != nil {
		return err
	}
	oldMemberIDs, err := listAnimeFranchiseMemberIDsByGroupIDsWithContext(ctx, tx, impactedGroupIDs)
	if err != nil {
		return err
	}
	for _, memberIDs := range oldMemberIDs {
		worklist = append(worklist, memberIDs...)
	}
	worklist = uniquePositiveIDs(worklist)

	coveredAnimeIDs := make(map[int]struct{}, len(worklist))
	seenGroupIDs := make(map[int]struct{}, len(worklist))
	components := make([]animeFranchiseComponent, 0, len(worklist))
	for _, seedID := range worklist {
		if _, ok := coveredAnimeIDs[seedID]; ok {
			continue
		}

		memberIDs, err := collectFranchiseMemberIDsWithTx(ctx, tx, seedID)
		if err != nil {
			return err
		}
		for _, memberID := range memberIDs {
			coveredAnimeIDs[memberID] = struct{}{}
		}

		// memberIDs is sorted ascending; its first element is the group's
		// representative and stable identifier.
		groupID := memberIDs[0]
		if _, ok := seenGroupIDs[groupID]; ok {
			continue
		}
		seenGroupIDs[groupID] = struct{}{}

		existingGroupIDs, err := listAnimeFranchiseGroupIDsByAnimeIDsWithContext(ctx, tx, memberIDs)
		if err != nil {
			return err
		}
		impactedGroupIDs = append(impactedGroupIDs, existingGroupIDs...)

		components = append(components, animeFranchiseComponent{
			MemberIDs: memberIDs,
			GroupID:   groupID,
		})
	}

	if err := deleteAnimeFranchisesByGroupIDsWithContext(ctx, tx, impactedGroupIDs); err != nil {
		return err
	}
	for _, component := range components {
		if err := upsertAnimeFranchiseMembersWithContext(ctx, tx, int64(component.GroupID), component.MemberIDs); err != nil {
			return err
		}
	}

	return nil
}

// collectFranchiseMemberIDsWithTx returns the sorted ids of every anime that
// belongs to the same franchise as seedID. It traverses anime_relations with a
// level-by-level BFS, fetching all frontier nodes in a single query per level
// (O(depth) round-trips) instead of one query per node.
func collectFranchiseMemberIDsWithTx(ctx context.Context, tx *sql.Tx, seedID int) ([]int, error) {
	ctx = ensureContext(ctx)

	componentIDs := make(map[int]struct{}, maxNodesPerFranchise)
	queue := []int{seedID}

	for len(queue) > 0 && len(componentIDs) < maxNodesPerFranchise {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Build this BFS level's frontier: unvisited nodes within the cap.
		frontier := make([]int, 0, len(queue))
		for _, id := range queue {
			if len(componentIDs)+len(frontier) >= maxNodesPerFranchise {
				break
			}
			if _, ok := componentIDs[id]; !ok {
				frontier = append(frontier, id)
			}
		}
		if len(frontier) == 0 {
			break
		}
		for _, id := range frontier {
			componentIDs[id] = struct{}{}
		}

		// One batch query for all frontier nodes instead of one per node.
		neighborsBySource, err := listUndirectedAnimeRelationIDsBatchWithContext(ctx, tx, frontier)
		if err != nil {
			return nil, err
		}

		seen := make(map[int]struct{})
		queue = queue[:0]
		for _, neighbors := range neighborsBySource {
			for _, neighbor := range neighbors {
				if _, ok := componentIDs[neighbor]; ok {
					continue
				}
				if _, ok := seen[neighbor]; ok {
					continue
				}
				seen[neighbor] = struct{}{}
				queue = append(queue, neighbor)
			}
		}
	}

	// A standalone anime with no relations forms its own single-member franchise.
	if len(componentIDs) == 0 {
		componentIDs[seedID] = struct{}{}
	}

	ids := make([]int, 0, len(componentIDs))
	for id := range componentIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}

// listAnimeFranchiseGroupIDsByAnimeIDsWithContext returns the distinct group ids
// (representative member ids) of the franchises the given anime currently belong
// to. Anime without a franchise row contribute nothing.
func listAnimeFranchiseGroupIDsByAnimeIDsWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) ([]int64, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return nil, nil
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT group_id
		FROM anime_franchises
		WHERE anime_id IN (%s)
		ORDER BY group_id
	`, BuildSQLPlaceholders(1, len(animeIDs))), IntsToAnySlice(animeIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groupIDs := make([]int64, 0)
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		groupIDs = append(groupIDs, groupID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groupIDs, nil
}

// listAnimeFranchiseMemberIDsByGroupIDsWithContext returns the member anime ids
// of each requested franchise, keyed by group id.
func listAnimeFranchiseMemberIDsByGroupIDsWithContext(ctx context.Context, tx *sql.Tx, groupIDs []int64) (map[int64][]int, error) {
	ctx = ensureContext(ctx)

	groupIDs = UniquePositiveInt64s(groupIDs)
	if len(groupIDs) == 0 {
		return map[int64][]int{}, nil
	}

	args := Int64sToAnySlice(groupIDs)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT group_id, anime_id
		FROM anime_franchises
		WHERE group_id IN (%s)
		ORDER BY group_id, anime_id
	`, BuildSQLPlaceholders(1, len(groupIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	memberIDs := make(map[int64][]int, len(groupIDs))
	for rows.Next() {
		var (
			groupID int64
			animeID int
		)
		if err := rows.Scan(&groupID, &animeID); err != nil {
			return nil, err
		}
		memberIDs[groupID] = append(memberIDs[groupID], animeID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return memberIDs, nil
}

func deleteAnimeFranchisesByGroupIDsWithContext(ctx context.Context, tx *sql.Tx, groupIDs []int64) error {
	ctx = ensureContext(ctx)

	groupIDs = UniquePositiveInt64s(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM anime_franchises
		WHERE group_id IN (%s)
	`, BuildSQLPlaceholders(1, len(groupIDs))), Int64sToAnySlice(groupIDs)...)
	return err
}

func upsertAnimeFranchiseMembersWithContext(ctx context.Context, tx *sql.Tx, groupID int64, animeIDs []int) error {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if groupID <= 0 || len(animeIDs) == 0 {
		return nil
	}

	rows := make([]string, 0, len(animeIDs))
	args := make([]any, 0, len(animeIDs)*2)
	argIndex := 1
	for _, animeID := range animeIDs {
		rows = append(rows, fmt.Sprintf("($%d, $%d)", argIndex, argIndex+1))
		args = append(args, animeID, groupID)
		argIndex += 2
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO anime_franchises (
			anime_id,
			group_id
		) VALUES %s
		ON CONFLICT (anime_id) DO UPDATE
		SET group_id = EXCLUDED.group_id
	`, strings.Join(rows, ", ")), args...)
	return err
}
