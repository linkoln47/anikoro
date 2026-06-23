package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

// UnresolvedCatalogRepository lists catalog stubs (resolved = false) that the
// lazy worker must hydrate, smallest id first.
type UnresolvedCatalogRepository interface {
	ListUnresolvedCatalogIDs(ctx context.Context, limit int) ([]int, error)
}

// LazyHydrationService resolves catalog stubs left behind by the lightweight
// user sync. The user sync only mirrors a MAL list into user_anime_items and
// upserts catalog stubs; this service fetches each stub's details (and its
// franchise neighbours) from MAL's public endpoint and persists them together
// with the rebuilt anime_franchises rows in a single atomic transaction via the
// hydration sink. It runs in the standalone lazy-worker, not in the user request path.
type LazyHydrationService struct {
	stubs         UnresolvedCatalogRepository
	hydrator      ports.AnimeCatalogHydrator
	franchiseRepo ports.FranchiseRepository
	catalogRepo   ports.AnimeCatalogRepository
	hydrationSink ports.AnimeCatalogHydrationSink
	cache         ports.DetailsCache
	failures      ports.AnimeHydrationFailureStore
	logger        ports.SyncLogger
}

type LazyHydrationServiceDependencies struct {
	Stubs         UnresolvedCatalogRepository
	Hydrator      ports.AnimeCatalogHydrator
	FranchiseRepo ports.FranchiseRepository
	CatalogRepo   ports.AnimeCatalogRepository
	// HydrationSink writes details and franchise groups atomically. Used by
	// ReplayStagedDetails to keep the replay path consistent with the normal
	// hydration path.
	HydrationSink ports.AnimeCatalogHydrationSink
	Cache         ports.DetailsCache
	Failures      ports.AnimeHydrationFailureStore
	Logger        ports.SyncLogger
}

func NewLazyHydrationService(deps LazyHydrationServiceDependencies) *LazyHydrationService {
	return &LazyHydrationService{
		stubs:         deps.Stubs,
		hydrator:      deps.Hydrator,
		franchiseRepo: deps.FranchiseRepo,
		catalogRepo:   deps.CatalogRepo,
		hydrationSink: deps.HydrationSink,
		cache:         deps.Cache,
		failures:      deps.Failures,
		logger:        deps.Logger,
	}
}

// ResolveStubs hydrates up to batchSize unresolved catalog stubs. For each
// stub it fetches details and franchise neighbours from MAL's public endpoint
// and persists them together with the rebuilt anime_franchises rows in a single
// atomic transaction (via the hydration sink injected into the hydrator). It
// returns how many stubs were resolved. A non-positive batchSize, or an empty
// queue, processes nothing.
func (service *LazyHydrationService) ResolveStubs(ctx context.Context, batchSize int) (int, error) {
	ctx = ensureContext(ctx)

	if batchSize <= 0 {
		return 0, nil
	}

	now := time.Now()
	scanLimit := batchSize
	if service.failures != nil {
		scanLimit += service.failures.DeferredCount(now)
	}

	candidateIDs, err := service.stubs.ListUnresolvedCatalogIDs(ctx, scanLimit)
	if err != nil {
		return 0, fmt.Errorf("cannot list unresolved catalog ids: %w", err)
	}
	stubIDs := make([]int, 0, batchSize)
	for _, animeID := range candidateIDs {
		if service.failures != nil && !service.failures.ShouldAttempt(animeID, now) {
			continue
		}
		stubIDs = append(stubIDs, animeID)
		if len(stubIDs) == batchSize {
			break
		}
	}
	if len(stubIDs) == 0 {
		return 0, nil
	}

	cacheStore, err := service.cache.OpenDetailsCache(ctx)
	if err != nil {
		service.warn("lazy_hydration", "cannot load details cache", "err", err)
	}
	defer func() {
		if err := cacheStore.FlushPending(); err != nil {
			service.warn("lazy_hydration", "cannot save details cache", "err", err)
		}
	}()

	reporter := newLoggingProgressReporter(service.logger)
	service.info("lazy_hydration", "hydrating catalog stubs", "count", len(stubIDs))
	if err := service.hydrator.HydratePublicCatalogGraph(ctx, stubIDs, cacheStore, reporter); err != nil {
		return 0, fmt.Errorf("cannot hydrate catalog stubs: %w", err)
	}

	// Count resolved for logging; franchise rows are already written atomically
	// inside HydratePublicCatalogGraph via the hydration sink.
	states, err := service.catalogRepo.GetAnimeCatalogStates(ctx, stubIDs)
	if err != nil {
		return 0, fmt.Errorf("cannot load hydrated catalog states: %w", err)
	}
	resolvedCount := 0
	for _, animeID := range stubIDs {
		if state, ok := states[animeID]; ok && state.Resolved {
			resolvedCount++
		}
	}

	service.info(
		"lazy_hydration",
		"hydrated catalog stubs",
		"requested", len(stubIDs),
		"hydrated", resolvedCount,
		"deferred", len(stubIDs)-resolvedCount,
	)
	return resolvedCount, nil
}

// ReconcileFranchises rebuilds anime_franchises rows for resolved catalog
// entries that have no franchise grouping — typically because a previous
// RefreshAnimeFranchises call failed after details were already persisted.
// It reads up to batchSize ungrouped ids and feeds them into
// RefreshAnimeFranchises, which traverses anime_relations to reconstruct the
// correct connected components (including standalone single-member groups).
// Returns the number of ids submitted for reconciliation.
func (service *LazyHydrationService) ReconcileFranchises(ctx context.Context, batchSize int) (int, error) {
	ctx = ensureContext(ctx)

	if batchSize <= 0 {
		return 0, nil
	}

	ungroupedIDs, err := service.catalogRepo.ListUngroupedResolvedCatalogIDs(ctx, batchSize)
	if err != nil {
		return 0, fmt.Errorf("cannot list ungrouped resolved catalog ids: %w", err)
	}
	if len(ungroupedIDs) == 0 {
		return 0, nil
	}

	service.info("lazy_hydration", "reconciling missing franchise groups", "count", len(ungroupedIDs))
	if err := service.franchiseRepo.RefreshAnimeFranchises(ctx, ungroupedIDs); err != nil {
		return 0, fmt.Errorf("cannot reconcile anime franchises: %w", err)
	}

	return len(ungroupedIDs), nil
}

// ReplayStagedDetails persists anime details that a previous worker run staged
// in the file cache but never wrote to the database, then clears the staging
// buffer. It is meant to run once on worker startup, before the resolve loop.
func (service *LazyHydrationService) ReplayStagedDetails(ctx context.Context) error {
	ctx = ensureContext(ctx)

	cacheStore, err := service.cache.OpenDetailsCache(ctx)
	if err != nil {
		service.warn("lazy_hydration", "cannot load details cache", "err", err)
	}
	defer func() {
		if err := cacheStore.FlushPending(); err != nil {
			service.warn("lazy_hydration", "cannot save details cache", "err", err)
		}
	}()

	return service.replayStagedAnimeDetails(ctx, cacheStore)
}

// replayStagedAnimeDetails persists details that a previous run staged in the
// file cache but never wrote to the database, then clears the staging buffer.
// It uses the hydration sink so that details and franchise groups are written
// atomically — the same guarantee as the normal hydration path.
func (service *LazyHydrationService) replayStagedAnimeDetails(ctx context.Context, cacheStore ports.AnimeDetailsCacheStore) error {
	staged := cacheStore.StagedDetails()
	if len(staged) == 0 {
		return nil
	}

	now := time.Now()
	stagedIDs := make([]int, 0, len(staged))
	fresh := make([]ports.CachedAnimeDetails, 0, len(staged))
	freshIDs := make([]int, 0, len(staged))
	for _, item := range staged {
		stagedIDs = append(stagedIDs, item.Details.ID)
		if item.Details.ID > 0 && item.IsFresh(now) {
			fresh = append(fresh, item)
			freshIDs = append(freshIDs, item.Details.ID)
		}
	}

	toSave := make([]domain.AnimeDetails, 0, len(fresh))
	if len(fresh) > 0 {
		states, err := service.catalogRepo.GetAnimeCatalogStates(ctx, freshIDs)
		if err != nil {
			return err
		}
		for _, item := range fresh {
			state, ok := states[item.Details.ID]
			if ok && state.Resolved && !state.DetailsSyncedAt.Before(item.UpdatedAt) {
				continue
			}
			toSave = append(toSave, item.Details)
		}
	}
	if len(toSave) > 0 {
		if err := service.hydrationSink.SaveAnimeCatalogDetailsWithFranchises(ctx, toSave); err != nil {
			return err
		}
		service.info("lazy_hydration", "replayed staged anime details into catalog", "count", len(toSave))
	}

	if err := cacheStore.MarkPersisted(stagedIDs); err != nil {
		return err
	}
	if service.failures != nil {
		return service.failures.MarkSucceeded(stagedIDs)
	}
	return nil
}

func (service *LazyHydrationService) info(component, msg string, args ...any) {
	if service != nil && service.logger != nil {
		service.logger.Info(component, msg, args...)
	}
}

func (service *LazyHydrationService) warn(component, msg string, args ...any) {
	if service != nil && service.logger != nil {
		service.logger.Warn(component, msg, args...)
	}
}

// loggingProgressReporter is a ports.SyncProgressReporter for background jobs:
// it logs phase progress for the operator instead of streaming SSE to a user.
// Throttled updates honour the requested interval so a large traversal does not
// flood the log.
type loggingProgressReporter struct {
	logger ports.SyncLogger

	mu         sync.Mutex
	lastUpdate time.Time
}

func newLoggingProgressReporter(logger ports.SyncLogger) *loggingProgressReporter {
	return &loggingProgressReporter{logger: logger}
}

func (reporter *loggingProgressReporter) Start(message string) {
	reporter.log("lazy_hydration", message)
}

func (reporter *loggingProgressReporter) Update(phase ports.SyncProgressPhase, current, total int, message string) {
	reporter.log("lazy_hydration", message, "phase", string(phase), "current", current, "total", total)
}

func (reporter *loggingProgressReporter) UpdateThrottled(phase ports.SyncProgressPhase, current, total int, message string, interval time.Duration) {
	reporter.mu.Lock()
	now := time.Now()
	if interval > 0 && !reporter.lastUpdate.IsZero() && now.Sub(reporter.lastUpdate) < interval {
		reporter.mu.Unlock()
		return
	}
	reporter.lastUpdate = now
	reporter.mu.Unlock()

	reporter.Update(phase, current, total, message)
}

func (reporter *loggingProgressReporter) Complete(message string) {
	reporter.log("lazy_hydration", message)
}

func (reporter *loggingProgressReporter) Fail(err error) {
	if reporter != nil && reporter.logger != nil && err != nil {
		reporter.logger.Error("lazy_hydration", "catalog hydration progress failed", "err", err)
	}
}

func (reporter *loggingProgressReporter) log(component, msg string, args ...any) {
	if reporter != nil && reporter.logger != nil {
		reporter.logger.Info(component, msg, args...)
	}
}
