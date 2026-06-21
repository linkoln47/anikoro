package usecase

import (
	"context"
	"fmt"
	"time"

	"test/internal/ports"
)

// StaleCatalogRepository lists catalog entries whose cached details have aged
// past a cutoff so they can be re-hydrated.
type StaleCatalogRepository interface {
	ListStaleCatalogIDs(ctx context.Context, before time.Time, limit int) ([]int, error)
}

// PublicCatalogBatchRefresher re-resolves catalog details for a flat list of
// anime ids over the public MAL endpoint (no franchise traversal).
type PublicCatalogBatchRefresher interface {
	RefreshPublicCatalogBatch(ctx context.Context, animeIDs []int, cache ports.AnimeDetailsCacheStore) ([]AnimeCatalogHydrationResult, error)
}

// CatalogRefreshService re-hydrates catalog entries whose details (and thus
// mal_score) have gone stale. The user sync only refreshes anime that appear in
// someone's list; this service keeps the rest of the catalog from drifting by
// refreshing the oldest entries on a schedule.
type CatalogRefreshService struct {
	catalog  StaleCatalogRepository
	hydrator PublicCatalogBatchRefresher
	cache    ports.DetailsCache
	logger   ports.SyncLogger
}

type CatalogRefreshServiceDependencies struct {
	Catalog  StaleCatalogRepository
	Hydrator PublicCatalogBatchRefresher
	Cache    ports.DetailsCache
	Logger   ports.SyncLogger
}

func NewCatalogRefreshService(deps CatalogRefreshServiceDependencies) *CatalogRefreshService {
	return &CatalogRefreshService{
		catalog:  deps.Catalog,
		hydrator: deps.Hydrator,
		cache:    deps.Cache,
		logger:   deps.Logger,
	}
}

// RefreshStaleCatalog re-hydrates up to `limit` catalog entries whose details
// were last synced more than `olderThan` ago, refreshing their mal_score (and
// the rest of their details) from MAL. It returns how many entries were
// processed. `olderThan` should be at least DetailsCacheTTL; below that the
// resolver still treats the entries as fresh and resolves them from the database
// without a re-fetch.
func (service *CatalogRefreshService) RefreshStaleCatalog(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	ctx = ensureContext(ctx)

	if limit <= 0 {
		return 0, nil
	}
	if olderThan < 0 {
		olderThan = 0
	}

	before := time.Now().Add(-olderThan)
	staleIDs, err := service.catalog.ListStaleCatalogIDs(ctx, before, limit)
	if err != nil {
		return 0, fmt.Errorf("cannot list stale catalog ids: %w", err)
	}
	if len(staleIDs) == 0 {
		service.info("catalog_refresh", "no stale catalog entries to refresh", "older_than", olderThan.String())
		return 0, nil
	}

	cacheStore, err := service.cache.OpenDetailsCache(ctx)
	if err != nil {
		service.warn("catalog_refresh", "cannot load details cache", "err", err)
	}
	defer func() {
		if err := cacheStore.FlushPending(); err != nil {
			service.warn("catalog_refresh", "cannot save details cache", "err", err)
		}
	}()

	service.info("catalog_refresh", "refreshing stale catalog entries", "count", len(staleIDs), "older_than", olderThan.String())
	if _, err := service.hydrator.RefreshPublicCatalogBatch(ctx, staleIDs, cacheStore); err != nil {
		return 0, fmt.Errorf("cannot refresh stale catalog details: %w", err)
	}

	service.info("catalog_refresh", "refreshed stale catalog entries", "count", len(staleIDs))
	return len(staleIDs), nil
}

func (service *CatalogRefreshService) info(component, msg string, args ...any) {
	if service != nil && service.logger != nil {
		service.logger.Info(component, msg, args...)
	}
}

func (service *CatalogRefreshService) warn(component, msg string, args ...any) {
	if service != nil && service.logger != nil {
		service.logger.Warn(component, msg, args...)
	}
}
