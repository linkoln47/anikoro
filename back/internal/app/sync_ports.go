package app

import (
	"sync"

	"test/internal/ports"
	"test/internal/usecase"
)

const DetailsCacheTTL = ports.DetailsCacheTTL

type CachedAnimeDetails = ports.CachedAnimeDetails
type AnimeDetailsFetchMode = ports.AnimeDetailsFetchMode
type MALAnimeClient = ports.MALAnimeClient
type DetailsCache = ports.DetailsCache
type AnimeDetailsCacheStore = ports.AnimeDetailsCacheStore
type SyncAnimeRepository = ports.SyncAnimeRepository
type UserAnimeRepository = ports.UserAnimeRepository
type AnimeCatalogRepository = ports.AnimeCatalogRepository
type FranchiseRepository = ports.FranchiseRepository
type AnimeCatalogHydrator = ports.AnimeCatalogHydrator
type SyncProgressReporter = ports.SyncProgressReporter
type UserSyncGuard = ports.UserSyncGuard
type SyncLogger = ports.SyncLogger
type animeCatalogHydrationResult = usecase.AnimeCatalogHydrationResult
type syncCatalogHydrator = usecase.SyncCatalogHydrator

const (
	animeDetailsFetchPrimary = ports.AnimeDetailsFetchPrimary
	animeDetailsFetchRetry   = ports.AnimeDetailsFetchRetry
)

func newSyncCatalogHydrator(mal MALAnimeClient, catalogRepo AnimeCatalogRepository, logger SyncLogger) *syncCatalogHydrator {
	return usecase.NewSyncCatalogHydrator(mal, catalogRepo, logger)
}

type inMemoryUserSyncGuard struct {
	mu                sync.Mutex
	activeUserSyncIDs map[int64]struct{}
}

func newInMemoryUserSyncGuard() *inMemoryUserSyncGuard {
	return &inMemoryUserSyncGuard{
		activeUserSyncIDs: make(map[int64]struct{}),
	}
}

func (guard *inMemoryUserSyncGuard) TryBeginUserSync(userID int64) bool {
	guard.mu.Lock()
	defer guard.mu.Unlock()

	if _, exists := guard.activeUserSyncIDs[userID]; exists {
		return false
	}

	guard.activeUserSyncIDs[userID] = struct{}{}
	return true
}

func (guard *inMemoryUserSyncGuard) FinishUserSync(userID int64) {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	delete(guard.activeUserSyncIDs, userID)
}

type appSyncLogger struct {
	app *App
}

func (logger appSyncLogger) Debug(component, msg string, args ...any) {
	logger.app.logDebug(component, msg, args...)
}

func (logger appSyncLogger) Info(component, msg string, args ...any) {
	logger.app.logInfo(component, msg, args...)
}

func (logger appSyncLogger) Warn(component, msg string, args ...any) {
	logger.app.logWarn(component, msg, args...)
}

func (logger appSyncLogger) Error(component, msg string, args ...any) {
	logger.app.logError(component, msg, args...)
}
