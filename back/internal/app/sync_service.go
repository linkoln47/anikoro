package app

import (
	"test/internal/adapters/postgres"
	"test/internal/usecase"
)

type SyncService = usecase.SyncService
type syncServiceDependencies = usecase.SyncServiceDependencies

func newSyncService(deps syncServiceDependencies) *SyncService {
	return usecase.NewSyncService(deps)
}

func newSyncServiceDependencies(app *App) syncServiceDependencies {
	logger := appSyncLogger{app: app}
	malClient := app.MALAnimeClient
	catalogRepo := postgres.NewCatalogRepository(app.DB)

	return syncServiceDependencies{
		MAL:              malClient,
		DetailsCache:     app.DetailsCache,
		CatalogRepo:      catalogRepo,
		UserAnimeRepo:    postgres.NewUserAnimeRepository(app.DB, logger),
		FranchiseRepo:    postgres.NewFranchiseRepository(app.DB, logger),
		CatalogHydrator:  newSyncCatalogHydrator(malClient, catalogRepo, logger),
		Guard:            app.SyncGuard,
		Logger:           logger,
		ClientIDProvider: appMALClientIDProvider{app: app},
	}
}

func (a *App) syncService() *SyncService {
	return a.Sync
}
