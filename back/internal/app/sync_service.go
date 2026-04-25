package app

import "test/internal/usecase"

type SyncService = usecase.SyncService
type syncServiceDependencies = usecase.SyncServiceDependencies

func newSyncService(deps syncServiceDependencies) *SyncService {
	return usecase.NewSyncService(deps)
}

func newSyncServiceDependencies(app *App) syncServiceDependencies {
	logger := appSyncLogger{app: app}
	malClient := app.MALAnimeClient
	catalogRepo := newPostgresCatalogRepository(app.DB)

	return syncServiceDependencies{
		MAL:              malClient,
		DetailsCache:     app.DetailsCache,
		CatalogRepo:      catalogRepo,
		UserAnimeRepo:    newPostgresUserAnimeRepository(app.DB, logger),
		FranchiseRepo:    newPostgresFranchiseRepository(app.DB, logger),
		CatalogHydrator:  newSyncCatalogHydrator(malClient, catalogRepo, logger),
		Guard:            app.SyncGuard,
		Logger:           logger,
		ClientIDProvider: appMALClientIDProvider{app: app},
	}
}

func (a *App) syncService() *SyncService {
	return a.Sync
}
