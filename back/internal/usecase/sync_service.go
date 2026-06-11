package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	DetailsCacheTTL = ports.DetailsCacheTTL

	SyncJobPhaseFetchingList     = ports.SyncJobPhaseFetchingList
	SyncJobPhaseListFetched      = ports.SyncJobPhaseListFetched
	SyncJobPhaseSavingSnapshot   = ports.SyncJobPhaseSavingSnapshot
	SyncJobPhaseHydratingCatalog = ports.SyncJobPhaseHydratingCatalog
	SyncJobPhaseGrouping         = ports.SyncJobPhaseGrouping
	SyncJobPhaseDone             = ports.SyncJobPhaseDone

	SyncJobProgressUpdateInterval = 2 * time.Second
)

type noopSyncProgressReporter struct{}

func (noopSyncProgressReporter) Start(string)                                     {}
func (noopSyncProgressReporter) Update(ports.SyncProgressPhase, int, int, string) {}
func (noopSyncProgressReporter) UpdateThrottled(ports.SyncProgressPhase, int, int, string, time.Duration) {
}
func (noopSyncProgressReporter) Complete(string) {}
func (noopSyncProgressReporter) Fail(error)      {}

func ensureSyncProgressReporter(reporter ports.SyncProgressReporter) ports.SyncProgressReporter {
	if reporter == nil {
		return noopSyncProgressReporter{}
	}
	return reporter
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func uniquePositiveIDs(ids []int) []int {
	unique := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return unique
}

type SyncService struct {
	mal             ports.MALAnimeClient
	detailsCache    ports.DetailsCache
	catalogRepo     ports.AnimeCatalogRepository
	userAnimeRepo   ports.UserAnimeRepository
	franchiseRepo   ports.FranchiseRepository
	catalogHydrator ports.AnimeCatalogHydrator
	guard           ports.UserSyncGuard
	logger          ports.SyncLogger
}

type SyncServiceDependencies struct {
	MAL             ports.MALAnimeClient
	DetailsCache    ports.DetailsCache
	AnimeRepo       ports.SyncAnimeRepository
	CatalogRepo     ports.AnimeCatalogRepository
	UserAnimeRepo   ports.UserAnimeRepository
	FranchiseRepo   ports.FranchiseRepository
	CatalogHydrator ports.AnimeCatalogHydrator
	Guard           ports.UserSyncGuard
	Logger          ports.SyncLogger
}

func NewSyncService(deps SyncServiceDependencies) *SyncService {
	if deps.CatalogRepo == nil {
		deps.CatalogRepo = deps.AnimeRepo
	}
	if deps.UserAnimeRepo == nil {
		deps.UserAnimeRepo = deps.AnimeRepo
	}
	if deps.FranchiseRepo == nil {
		deps.FranchiseRepo = deps.AnimeRepo
	}
	if deps.CatalogHydrator == nil && deps.MAL != nil && deps.CatalogRepo != nil {
		deps.CatalogHydrator = NewSyncCatalogHydrator(deps.MAL, deps.CatalogRepo, deps.Logger)
	}

	return &SyncService{
		mal:             deps.MAL,
		detailsCache:    deps.DetailsCache,
		catalogRepo:     deps.CatalogRepo,
		userAnimeRepo:   deps.UserAnimeRepo,
		franchiseRepo:   deps.FranchiseRepo,
		catalogHydrator: deps.CatalogHydrator,
		guard:           deps.Guard,
		logger:          deps.Logger,
	}
}

func (service *SyncService) RunSyncWithJob(ctx context.Context, userID int64, token string, reporter ports.SyncProgressReporter) {
	ctx = ensureContext(ctx)
	reporter = ensureSyncProgressReporter(reporter)

	if !service.guard.TryBeginUserSync(userID) {
		err := errors.New("sync is already running for this user")
		reporter.Fail(err)
		service.logger.Warn("sync", "MAL sync skipped because another sync is already running", "user_id", userID)
		return
	}
	defer service.guard.FinishUserSync(userID)

	service.logger.Info("sync", "MAL sync started", "user_id", userID)
	reporter.Start("Fetching MAL anime list")
	if err := service.SyncAnimeWithProgressContext(ctx, userID, token, reporter); err != nil {
		reporter.Fail(err)
		service.logger.Error("sync", "MAL sync failed", "user_id", userID, "err", err)
		return
	}
	reporter.Complete("Sync completed")
	service.logger.Info("sync", "MAL sync completed", "user_id", userID)
}

func (service *SyncService) RunPublicSyncWithJob(ctx context.Context, userID int64, username string, reporter ports.SyncProgressReporter) {
	ctx = ensureContext(ctx)
	reporter = ensureSyncProgressReporter(reporter)

	if !service.guard.TryBeginUserSync(userID) {
		err := errors.New("sync is already running for this user")
		reporter.Fail(err)
		service.logger.Warn("sync", "public MAL sync skipped because another sync is already running", "username", username, "user_id", userID)
		return
	}
	defer service.guard.FinishUserSync(userID)

	service.logger.Info("sync", "public MAL sync started", "username", username, "user_id", userID)
	reporter.Start("Fetching public MAL list")
	if err := service.SyncPublicAnimeWithProgressContext(ctx, userID, username, reporter); err != nil {
		reporter.Fail(err)
		service.logger.Error("sync", "public MAL sync failed", "username", username, "user_id", userID, "err", err)
		return
	}
	reporter.Complete("Public sync completed")
	service.logger.Info("sync", "public MAL sync completed", "username", username, "user_id", userID)
}

func (service *SyncService) SyncAnimeWithProgressContext(ctx context.Context, userID int64, token string, reporter ports.SyncProgressReporter) error {
	ctx = ensureContext(ctx)
	reporter = ensureSyncProgressReporter(reporter)

	reporter.Update(SyncJobPhaseFetchingList, 0, 0, "Fetching MAL anime list")
	allEntries, err := service.mal.FetchAnimeList(ctx, token)
	if err != nil {
		return err
	}
	reporter.Update(SyncJobPhaseListFetched, len(allEntries), len(allEntries), fmt.Sprintf("Fetched %d anime list entries", len(allEntries)))

	return service.SyncAnimeEntriesWithTokenContext(ctx, userID, allEntries, token, reporter)
}

func (service *SyncService) SyncPublicAnimeWithProgressContext(ctx context.Context, userID int64, username string, reporter ports.SyncProgressReporter) error {
	ctx = ensureContext(ctx)
	reporter = ensureSyncProgressReporter(reporter)

	reporter.Update(SyncJobPhaseFetchingList, 0, 0, "Fetching public MAL list")
	allEntries, err := service.mal.FetchPublicAnimeList(ctx, username)
	if err != nil {
		return err
	}
	reporter.Update(SyncJobPhaseListFetched, len(allEntries), len(allEntries), fmt.Sprintf("Fetched %d public anime list entries", len(allEntries)))

	return service.SyncPublicAnimeEntriesContext(ctx, userID, allEntries, reporter)
}

func (service *SyncService) SyncAnimeEntriesWithTokenContext(ctx context.Context, userID int64, allEntries []domain.UserAnimeListEntry, token string, reporter ports.SyncProgressReporter) error {
	return service.syncAnimeEntriesContext(ctx, userID, allEntries, reporter, func(ctx context.Context, entryIDs []int, cacheStore ports.AnimeDetailsCacheStore, reporter ports.SyncProgressReporter) error {
		return service.catalogHydrator.HydrateCatalogGraph(ctx, token, entryIDs, cacheStore, reporter)
	})
}

func (service *SyncService) SyncPublicAnimeEntriesContext(ctx context.Context, userID int64, allEntries []domain.UserAnimeListEntry, reporter ports.SyncProgressReporter) error {
	return service.syncAnimeEntriesContext(ctx, userID, allEntries, reporter, func(ctx context.Context, entryIDs []int, cacheStore ports.AnimeDetailsCacheStore, reporter ports.SyncProgressReporter) error {
		return service.catalogHydrator.HydratePublicCatalogGraph(ctx, entryIDs, cacheStore, reporter)
	})
}

func (service *SyncService) syncAnimeEntriesContext(
	ctx context.Context,
	userID int64,
	allEntries []domain.UserAnimeListEntry,
	reporter ports.SyncProgressReporter,
	hydrateCatalog func(context.Context, []int, ports.AnimeDetailsCacheStore, ports.SyncProgressReporter) error,
) error {
	ctx = ensureContext(ctx)
	reporter = ensureSyncProgressReporter(reporter)

	allEntries, duplicateCount := domain.DeduplicateUserAnimeListEntriesPreserveOrder(allEntries)
	if duplicateCount > 0 {
		service.logger.Warn("sync", "dropped duplicate MAL anime list entries before sync", "user_id", userID, "count", duplicateCount)
	}
	if len(allEntries) == 0 {
		reporter.Update(SyncJobPhaseSavingSnapshot, 0, 0, "Clearing empty local snapshot")
		if err := service.userAnimeRepo.ClearUserAnimeSnapshot(ctx, userID); err != nil {
			return fmt.Errorf("cannot clear empty user snapshot: %w", err)
		}
		service.logger.Info("sync", "no anime list entries found, cleared user snapshot", "user_id", userID)
		return nil
	}

	cacheStore, err := service.detailsCache.OpenDetailsCache(ctx)
	if err != nil {
		service.logger.Warn("sync", "cannot load details cache", "err", err)
	}
	defer func() {
		if err := cacheStore.FlushPending(); err != nil {
			service.logger.Warn("sync", "cannot save details cache", "err", err)
		}
	}()

	if err := service.replayStagedAnimeDetails(ctx, cacheStore); err != nil {
		service.logger.Warn("sync", "cannot replay staged anime details into catalog", "err", err)
	}

	entryIDs := domain.UniqueUserAnimeListEntryIDs(allEntries)
	reporter.Update(SyncJobPhaseSavingSnapshot, 0, len(entryIDs), "Saving local anime snapshot")
	if err := service.catalogRepo.UpsertAnimeCatalogStubs(ctx, entryIDs); err != nil {
		return fmt.Errorf("cannot upsert anime catalog stubs: %w", err)
	}

	reporter.Update(SyncJobPhaseHydratingCatalog, 0, len(entryIDs), "Syncing anime details")
	if err := hydrateCatalog(ctx, entryIDs, cacheStore, reporter); err != nil {
		return fmt.Errorf("cannot hydrate anime catalog graph: %w", err)
	}

	reporter.Update(SyncJobPhaseGrouping, len(entryIDs), len(entryIDs), "Updating global anime franchises")
	if err := service.franchiseRepo.RefreshAnimeFranchises(ctx, entryIDs); err != nil {
		return fmt.Errorf("cannot refresh global anime franchises: %w", err)
	}

	reporter.Update(SyncJobPhaseSavingSnapshot, len(entryIDs), len(entryIDs), "Saving local anime snapshot")
	if err := service.userAnimeRepo.ReplaceUserAnimeItems(ctx, userID, allEntries); err != nil {
		return fmt.Errorf("cannot save user anime items: %w", err)
	}

	reporter.Update(SyncJobPhaseDone, len(entryIDs), len(entryIDs), "Finalizing sync")
	return nil
}

// replayStagedAnimeDetails persists details that a previous run staged in the
// file cache but never wrote to the database, then clears the staging buffer.
func (service *SyncService) replayStagedAnimeDetails(ctx context.Context, cacheStore ports.AnimeDetailsCacheStore) error {
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
		if err := service.catalogRepo.SaveAnimeCatalogDetailsBatch(ctx, toSave); err != nil {
			return err
		}
		service.logger.Info("sync", "replayed staged anime details into catalog", "count", len(toSave))
	}

	return cacheStore.MarkPersisted(stagedIDs)
}
