package app

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"test/internal/domain"
)

type PostgresSyncAnimeRepository struct {
	catalog   *PostgresCatalogRepository
	userAnime *PostgresUserAnimeRepository
	franchise *PostgresFranchiseRepository
}

type PostgresCatalogRepository struct {
	db *sql.DB
}

type PostgresUserAnimeRepository struct {
	db     *sql.DB
	logger SyncLogger
}

type PostgresFranchiseRepository struct {
	db     *sql.DB
	logger SyncLogger
}

type animeFranchiseComponent struct {
	MemberIDs []int
	MemberKey string
}

func newPostgresSyncAnimeRepository(db *sql.DB, logger SyncLogger) *PostgresSyncAnimeRepository {
	return &PostgresSyncAnimeRepository{
		catalog:   newPostgresCatalogRepository(db),
		userAnime: newPostgresUserAnimeRepository(db, logger),
		franchise: newPostgresFranchiseRepository(db, logger),
	}
}

func newPostgresCatalogRepository(db *sql.DB) *PostgresCatalogRepository {
	return &PostgresCatalogRepository{db: db}
}

func newPostgresUserAnimeRepository(db *sql.DB, logger SyncLogger) *PostgresUserAnimeRepository {
	return &PostgresUserAnimeRepository{db: db, logger: logger}
}

func newPostgresFranchiseRepository(db *sql.DB, logger SyncLogger) *PostgresFranchiseRepository {
	return &PostgresFranchiseRepository{db: db, logger: logger}
}

func (repo *PostgresSyncAnimeRepository) ClearUserAnimeSnapshot(ctx context.Context, userID int64) error {
	return repo.userAnime.ClearUserAnimeSnapshot(ctx, userID)
}

func (repo *PostgresSyncAnimeRepository) UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error {
	return repo.catalog.UpsertAnimeCatalogStubs(ctx, animeIDs)
}

func (repo *PostgresSyncAnimeRepository) GetAnimeCatalogState(ctx context.Context, animeID int) (AnimeCatalogState, bool, error) {
	return repo.catalog.GetAnimeCatalogState(ctx, animeID)
}

func (repo *PostgresSyncAnimeRepository) GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]AnimeCatalogState, error) {
	return repo.catalog.GetAnimeCatalogStates(ctx, animeIDs)
}

func (repo *PostgresSyncAnimeRepository) GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error) {
	return repo.catalog.GetAnimeCatalogMediaType(ctx, animeID)
}

func (repo *PostgresSyncAnimeRepository) ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	return repo.catalog.ListAnimeRelationIDs(ctx, animeID)
}

func (repo *PostgresSyncAnimeRepository) ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error) {
	return repo.catalog.ListAnimeRelationIDsBySourceIDs(ctx, animeIDs)
}

func (repo *PostgresSyncAnimeRepository) ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	return repo.catalog.ListUndirectedAnimeRelationIDs(ctx, animeID)
}

func (repo *PostgresSyncAnimeRepository) SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []AnimeDetails) error {
	return repo.catalog.SaveAnimeCatalogDetailsBatch(ctx, detailsBatch)
}

func (repo *PostgresSyncAnimeRepository) RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error {
	return repo.franchise.RefreshAnimeFranchises(ctx, seedIDs)
}

func (repo *PostgresSyncAnimeRepository) ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []CompletedAnimeEntry) error {
	return repo.userAnime.ReplaceUserAnimeItems(ctx, userID, entries)
}

func (repo *PostgresCatalogRepository) UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error {
	return withTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		return upsertAnimeCatalogStubsWithTx(ctx, tx, animeIDs)
	})
}

func (repo *PostgresCatalogRepository) GetAnimeCatalogState(ctx context.Context, animeID int) (AnimeCatalogState, bool, error) {
	ctx = ensureContext(ctx)

	var state AnimeCatalogState
	err := repo.db.QueryRowContext(ctx, `
		SELECT id, resolved, COALESCE(details_synced_at, TIMESTAMPTZ 'epoch')
		FROM anime_catalog
		WHERE id = $1
	`, animeID).Scan(&state.AnimeID, &state.Resolved, &state.DetailsSyncedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return AnimeCatalogState{}, false, nil
		}
		return AnimeCatalogState{}, false, err
	}

	return state, true, nil
}

func (repo *PostgresCatalogRepository) GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]AnimeCatalogState, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]AnimeCatalogState{}, nil
	}

	args := intsToAnySlice(animeIDs)
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, resolved, COALESCE(details_synced_at, TIMESTAMPTZ 'epoch')
		FROM anime_catalog
		WHERE id IN (%s)
	`, buildSQLPlaceholders(1, len(animeIDs))), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[int]AnimeCatalogState, len(animeIDs))
	for rows.Next() {
		var state AnimeCatalogState
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

func (repo *PostgresCatalogRepository) GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error) {
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

func (repo *PostgresCatalogRepository) ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	ctx = ensureContext(ctx)

	rows, err := repo.db.QueryContext(ctx, `
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

func (repo *PostgresCatalogRepository) ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int][]int{}, nil
	}

	args := intsToAnySlice(animeIDs)
	rows, err := repo.db.QueryContext(ctx, fmt.Sprintf(`
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

func (repo *PostgresCatalogRepository) ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error) {
	return listUndirectedAnimeRelationIDsWithContext(ctx, repo.db, animeID)
}

func (repo *PostgresCatalogRepository) SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []AnimeDetails) error {
	ctx = ensureContext(ctx)

	normalized, err := normalizeAnimeCatalogDetailsBatch(detailsBatch)
	if err != nil {
		return err
	}
	if len(normalized) == 0 {
		return nil
	}

	return withTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
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

func listCatalogItemsByIDsWithContext(ctx context.Context, tx *sql.Tx, animeIDs []int) (map[int]FranchiseEntry, error) {
	ctx = ensureContext(ctx)

	animeIDs = uniquePositiveIDs(animeIDs)
	if len(animeIDs) == 0 {
		return map[int]FranchiseEntry{}, nil
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

	items := make(map[int]FranchiseEntry, len(animeIDs))
	for rows.Next() {
		var item FranchiseEntry
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

func listRelationsBySourceIDsWithContext(ctx context.Context, tx *sql.Tx, sourceIDs []int) (map[int]map[int]AnimeRelation, error) {
	ctx = ensureContext(ctx)

	sourceIDs = uniquePositiveIDs(sourceIDs)
	if len(sourceIDs) == 0 {
		return map[int]map[int]AnimeRelation{}, nil
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

	relationMap := make(map[int]map[int]AnimeRelation, len(sourceIDs))
	for rows.Next() {
		var (
			sourceID int
			relation AnimeRelation
		)
		if err := rows.Scan(&sourceID, &relation.ID, &relation.RelationType); err != nil {
			return nil, err
		}
		relation.RelationTypeFormatted = domain.FormatAnimeRelationType(relation.RelationType)

		targets := relationMap[sourceID]
		if targets == nil {
			targets = make(map[int]AnimeRelation)
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

func upsertAnimeCatalogDetailsBatchWithTx(ctx context.Context, tx *sql.Tx, detailsBatch []AnimeDetails, syncedAt time.Time) error {
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

func replaceAnimeRelationsBatchWithTx(ctx context.Context, tx *sql.Tx, detailsBatch []AnimeDetails) error {
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

func collectAnimeCatalogStubIDs(detailsBatch []AnimeDetails) []int {
	ids := make([]int, 0, len(detailsBatch))
	for _, details := range detailsBatch {
		if details.ID > 0 {
			ids = append(ids, details.ID)
		}
		ids = append(ids, details.RelatedIDs...)
	}

	return uniquePositiveIDs(ids)
}

func normalizeAnimeCatalogDetailsBatch(detailsBatch []AnimeDetails) ([]AnimeDetails, error) {
	if len(detailsBatch) == 0 {
		return nil, nil
	}

	normalizedByID := make(map[int]AnimeDetails, len(detailsBatch))
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

	normalized := make([]AnimeDetails, 0, len(order))
	for _, animeID := range order {
		normalized = append(normalized, normalizedByID[animeID])
	}
	return normalized, nil
}

func (repo *PostgresUserAnimeRepository) ClearUserAnimeSnapshot(ctx context.Context, userID int64) error {
	ctx = ensureContext(ctx)

	return withUserTx(ctx, repo.db, userID, nil, func(tx *sql.Tx) error {
		if repo.logger != nil {
			repo.logger.Info("db", "clearing empty user anime snapshot", "user_id", userID)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM user_anime_items WHERE user_id = $1`, userID); err != nil {
			return err
		}

		return nil
	})
}

func (repo *PostgresUserAnimeRepository) ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []CompletedAnimeEntry) error {
	ctx = ensureContext(ctx)

	return withUserTx(ctx, repo.db, userID, nil, func(tx *sql.Tx) error {
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

func (repo *PostgresFranchiseRepository) RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error {
	ctx = ensureContext(ctx)

	seedIDs = uniquePositiveIDs(seedIDs)
	if len(seedIDs) == 0 {
		return nil
	}

	return withTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		if repo.logger != nil {
			repo.logger.Info("db", "refreshing global anime franchises", "table", animeFranchisesTableName, "seed_count", len(seedIDs))
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

func (repo *PostgresFranchiseRepository) collectFranchiseIDsWithContext(ctx context.Context, tx *sql.Tx, seedIDs []int) ([]int, error) {
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
