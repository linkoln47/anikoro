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

// SyncService runs the lightweight user sync (the hot path): it mirrors a MAL
// list into user_anime_items and upserts catalog stubs for any new ids. It does
// not hydrate catalog details or rebuild franchises — the standalone lazy-worker
// resolves the stubs it leaves behind (see LazyHydrationService).
type SyncService struct {
	mal           ports.MALAnimeClient
	catalogRepo   ports.AnimeCatalogRepository
	userAnimeRepo ports.UserAnimeRepository
	guard         ports.UserSyncGuard
	logger        ports.SyncLogger
}

type SyncServiceDependencies struct {
	MAL           ports.MALAnimeClient
	AnimeRepo     ports.SyncAnimeRepository
	CatalogRepo   ports.AnimeCatalogRepository
	UserAnimeRepo ports.UserAnimeRepository
	Guard         ports.UserSyncGuard
	Logger        ports.SyncLogger
}

func NewSyncService(deps SyncServiceDependencies) *SyncService {
	if deps.CatalogRepo == nil {
		deps.CatalogRepo = deps.AnimeRepo
	}
	if deps.UserAnimeRepo == nil {
		deps.UserAnimeRepo = deps.AnimeRepo
	}

	return &SyncService{
		mal:           deps.MAL,
		catalogRepo:   deps.CatalogRepo,
		userAnimeRepo: deps.UserAnimeRepo,
		guard:         deps.Guard,
		logger:        deps.Logger,
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

func (service *SyncService) SyncAnimeWithProgressContext(ctx context.Context, userID int64, token string, reporter ports.SyncProgressReporter) error {
	ctx = ensureContext(ctx)
	reporter = ensureSyncProgressReporter(reporter)

	reporter.Update(SyncJobPhaseFetchingList, 0, 0, "Fetching MAL anime list")
	allEntries, err := service.mal.FetchAnimeList(ctx, token)
	if err != nil {
		return err
	}
	reporter.Update(SyncJobPhaseListFetched, len(allEntries), len(allEntries), fmt.Sprintf("Fetched %d anime list entries", len(allEntries)))

	return service.syncAnimeEntriesContext(ctx, userID, allEntries, reporter)
}

// syncAnimeEntriesContext mirrors the fetched MAL list into the local snapshot.
// It upserts catalog stubs for every id (new ids stay resolved=false until the
// lazy-worker hydrates them) and replaces the user's anime items. It does not
// fetch details or rebuild franchises, so the user request returns immediately.
func (service *SyncService) syncAnimeEntriesContext(
	ctx context.Context,
	userID int64,
	allEntries []domain.UserAnimeListEntry,
	reporter ports.SyncProgressReporter,
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

	entryIDs := domain.UniqueUserAnimeListEntryIDs(allEntries)
	reporter.Update(SyncJobPhaseSavingSnapshot, 0, len(entryIDs), "Saving local anime snapshot")
	if err := service.catalogRepo.UpsertAnimeCatalogStubs(ctx, entryIDs); err != nil {
		return fmt.Errorf("cannot upsert anime catalog stubs: %w", err)
	}

	if err := service.userAnimeRepo.ReplaceUserAnimeItems(ctx, userID, allEntries); err != nil {
		return fmt.Errorf("cannot save user anime items: %w", err)
	}

	reporter.Update(SyncJobPhaseDone, len(entryIDs), len(entryIDs), "Finalizing sync")
	return nil
}
