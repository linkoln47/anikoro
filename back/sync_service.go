package main

import "test/internal/usecase"

type SyncService = usecase.SyncService
type syncServiceDependencies = usecase.SyncServiceDependencies

func newSyncService(deps syncServiceDependencies) *SyncService {
	return usecase.NewSyncService(deps)
}

func newSyncServiceDependencies(app *App) syncServiceDependencies {
	logger := appSyncLogger{app: app}
	malClient := app.malClient()
	catalogRepo := newPostgresCatalogRepository(app.DB)

	return syncServiceDependencies{
		MAL:              malClient,
		DetailsCache:     app.detailsCache(),
		CatalogRepo:      catalogRepo,
		UserAnimeRepo:    newPostgresUserAnimeRepository(app.DB, logger),
		FranchiseRepo:    newPostgresFranchiseRepository(app.DB, logger),
		CatalogHydrator:  newSyncCatalogHydrator(malClient, catalogRepo, logger),
		Guard:            appUserSyncGuard{app: app},
		Logger:           logger,
		ClientIDProvider: appMALClientIDProvider{app: app},
	}
}

func (a *App) syncService() *SyncService {
	if a.Sync != nil {
		return a.Sync
	}
	a.Sync = newSyncService(newSyncServiceDependencies(a))
	return a.Sync
}
