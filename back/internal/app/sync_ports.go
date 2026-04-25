package app

import (
	"sync"

	"test/internal/usecase"
)

const DetailsCacheTTL = usecase.DetailsCacheTTL

type MALAuth = usecase.MALAuth
type CachedAnimeDetails = usecase.CachedAnimeDetails
type AnimeDetailsFetchMode = usecase.AnimeDetailsFetchMode
type MALAnimeClient = usecase.MALAnimeClient
type DetailsCache = usecase.DetailsCache
type AnimeDetailsCacheStore = usecase.AnimeDetailsCacheStore
type SyncAnimeRepository = usecase.SyncAnimeRepository
type UserAnimeRepository = usecase.UserAnimeRepository
type AnimeCatalogRepository = usecase.AnimeCatalogRepository
type FranchiseRepository = usecase.FranchiseRepository
type AnimeCatalogHydrator = usecase.AnimeCatalogHydrator
type SyncProgressReporter = usecase.SyncProgressReporter
type UserSyncGuard = usecase.UserSyncGuard
type SyncLogger = usecase.SyncLogger
type MALClientIDProvider = usecase.MALClientIDProvider
type animeCatalogHydrationResult = usecase.AnimeCatalogHydrationResult
type syncCatalogHydrator = usecase.SyncCatalogHydrator

const (
	animeDetailsFetchPrimary = usecase.AnimeDetailsFetchPrimary
	animeDetailsFetchRetry   = usecase.AnimeDetailsFetchRetry
)

func bearerMALAuth(token string) MALAuth {
	return usecase.BearerMALAuth(token)
}

func clientIDMALAuth(clientID string) MALAuth {
	return usecase.ClientIDMALAuth(clientID)
}

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

type appMALClientIDProvider struct {
	app *App
}

func (provider appMALClientIDProvider) MALClientID() string {
	return provider.app.Config.ClientID
}
