package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"test/internal/adapters/crypto"
	"test/internal/adapters/filecache"
	"test/internal/adapters/mal"
	"test/internal/adapters/postgres"
	"test/internal/httpapi"
	"test/internal/ports"
	"test/internal/usecase"
)

type App struct {
	Config     AppConfig
	DB         *sql.DB
	HTTPClient *http.Client
	Logger     *slog.Logger

	AnimeQueries   *usecase.AnimeQueryService
	SeasonQueries  *usecase.SeasonQueryService
	Sync           *usecase.SyncService
	ListEdits      *usecase.ListEditService
	MALAnimeClient ports.MALAnimeClient
	DetailsCache   ports.DetailsCache
	SyncJobs       httpapi.SyncJobStore
	Auth           *usecase.AuthService
	SyncGuard      ports.UserSyncGuard
}

func NewApp() *App {
	cfg := LoadConfig()

	app := &App{
		Config: cfg,
		Logger: newLogger(cfg),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	return app
}

func (a *App) compose() error {
	if a.DB == nil {
		return errors.New("compose app requires an open database")
	}
	if a.Logger == nil {
		a.Logger = newLogger(a.Config)
	}
	if a.HTTPClient == nil {
		a.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}

	logger := appSyncLogger{app: a}
	a.MALAnimeClient = mal.NewAnimeClient(a.HTTPClient, a.Config.ClientID, logger)
	a.DetailsCache = filecache.NewDetailsCache(a.Config.DetailsCachePath, filecache.DetailsCacheFlushBatch, logger)
	a.SyncJobs = httpapi.NewInMemorySyncJobStore()
	a.SyncGuard = newInMemoryUserSyncGuard()

	catalogRepo := postgres.NewCatalogRepository(a.DB)
	a.Auth = usecase.NewAuthService(usecase.AuthServiceDependencies{
		Repo:   postgres.NewAuthRepository(a.DB),
		Hasher: crypto.NewBcryptHasher(0),
		OAuth:  mal.NewOAuthClient(a.HTTPClient),
		OAuthConfig: ports.MALOAuthConfig{
			ClientID:     a.Config.ClientID,
			ClientSecret: a.Config.ClientSecret,
			RedirectURI:  a.Config.RedirectURI,
		},
	})
	animeRepo := postgres.NewAnimeRepository(a.DB)
	a.AnimeQueries = usecase.NewAnimeQueryService(animeRepo)
	a.SeasonQueries = usecase.NewSeasonQueryService(animeRepo)
	malListWriter, ok := a.MALAnimeClient.(ports.MALAnimeListWriter)
	if !ok {
		return errors.New("MAL anime client does not support list updates")
	}
	a.ListEdits = usecase.NewListEditService(usecase.ListEditServiceDependencies{
		MALWriter:     malListWriter,
		CatalogRepo:   catalogRepo,
		UserAnimeRepo: postgres.NewUserAnimeRepository(a.DB, logger),
		Logger:        logger,
	})
	a.Sync = usecase.NewSyncService(usecase.SyncServiceDependencies{
		MAL:             a.MALAnimeClient,
		DetailsCache:    a.DetailsCache,
		CatalogRepo:     catalogRepo,
		UserAnimeRepo:   postgres.NewUserAnimeRepository(a.DB, logger),
		FranchiseRepo:   postgres.NewFranchiseRepository(a.DB, logger),
		CatalogHydrator: usecase.NewSyncCatalogHydrator(a.MALAnimeClient, catalogRepo, logger),
		Guard:           a.SyncGuard,
		Logger:          logger,
	})

	return nil
}

// RunCatalogRefresh re-hydrates up to `limit` catalog entries whose details are
// older than `olderThan`, refreshing their mal_score (and the rest of their
// details) from MAL's public endpoint. It composes only the dependencies the
// refresh needs — no HTTP server, auth or sync wiring — so it can run as a
// standalone scheduled command. It returns the number of entries processed.
func (a *App) RunCatalogRefresh(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.Config.ClientID == "" {
		return 0, errors.New("catalog refresh requires MAL_CLIENT_ID for public anime detail lookups")
	}
	if err := a.OpenDB(); err != nil {
		return 0, err
	}
	if a.Logger == nil {
		a.Logger = newLogger(a.Config)
	}
	if a.HTTPClient == nil {
		a.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}

	logger := appSyncLogger{app: a}
	malClient := mal.NewAnimeClient(a.HTTPClient, a.Config.ClientID, logger)
	detailsCache := filecache.NewDetailsCache(a.Config.DetailsCachePath, filecache.DetailsCacheFlushBatch, logger)
	catalogRepo := postgres.NewCatalogRepository(a.DB)
	hydrator := usecase.NewSyncCatalogHydrator(malClient, catalogRepo, logger)
	service := usecase.NewCatalogRefreshService(usecase.CatalogRefreshServiceDependencies{
		Catalog:  catalogRepo,
		Hydrator: hydrator,
		Cache:    detailsCache,
		Logger:   logger,
	})

	return service.RefreshStaleCatalog(ctx, olderThan, limit)
}

func (a *App) OpenDB() error {
	if a.DB != nil {
		return nil
	}

	db, err := postgres.OpenDB(a.Config.DatabaseURL)
	if err != nil {
		return err
	}

	a.DB = db
	return nil
}

func (a *App) Close() error {
	if a.DB == nil {
		return nil
	}

	err := a.DB.Close()
	a.DB = nil
	return err
}
