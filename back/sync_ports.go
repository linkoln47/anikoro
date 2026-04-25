package main

import "test/internal/usecase"

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

type appUserSyncGuard struct {
	app *App
}

func (guard appUserSyncGuard) TryBeginUserSync(userID int64) bool {
	return guard.app.tryBeginUserSync(userID)
}

func (guard appUserSyncGuard) FinishUserSync(userID int64) {
	guard.app.finishUserSync(userID)
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
