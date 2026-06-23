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
		MAL:           a.MALAnimeClient,
		CatalogRepo:   catalogRepo,
		UserAnimeRepo: postgres.NewUserAnimeRepository(a.DB, logger),
		Guard:         a.SyncGuard,
		Logger:        logger,
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

// LazyWorkerConfig tunes the standalone lazy-worker. Interval paces the cycles,
// BatchSize bounds MAL calls per cycle, TTL decides when resolved details count
// as stale, and Once runs a single cycle and returns (for bootstrap/cron use).
type LazyWorkerConfig struct {
	Interval  time.Duration
	BatchSize int
	TTL       time.Duration
	Once      bool
}

// RunLazyWorker runs the cold-path catalog worker. Each cycle it (A) hydrates
// unresolved catalog stubs left behind by the lightweight user sync — fetching
// their details and franchise neighbours from MAL's public endpoint and
// rebuilding the affected anime_franchises rows — then (B) re-fetches resolved
// entries whose details (and mal_score) have gone stale. It composes only the
// dependencies the worker needs (no HTTP server, auth, or user sync wiring) so
// it can run as a standalone long-lived container. With cfg.Once it runs a
// single cycle and returns.
func (a *App) RunLazyWorker(ctx context.Context, cfg LazyWorkerConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if a.Config.ClientID == "" {
		return errors.New("lazy worker requires MAL_CLIENT_ID for public anime detail lookups")
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultLazyWorkerBatchSize
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultLazyWorkerInterval
	}
	if cfg.TTL < 0 {
		cfg.TTL = 0
	}
	if err := a.OpenDB(); err != nil {
		return err
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
	failureCache := filecache.NewHydrationFailureCache(a.Config.HydrationFailureCachePath, logger)
	failureStore, err := failureCache.Open(ctx)
	if err != nil {
		logger.Warn("lazy_worker", "cannot load anime hydration failure cache", "err", err)
	}
	catalogRepo := postgres.NewCatalogRepository(a.DB)
	// syncRepo composes catalog + franchise repos and is used as the hydration
	// sink: it writes details and franchise groups in a single atomic transaction.
	syncRepo := postgres.NewSyncAnimeRepository(a.DB, logger)
	hydrator := usecase.NewSyncCatalogHydratorForLazyWorker(malClient, catalogRepo, syncRepo, failureStore, logger)

	lazyHydration := usecase.NewLazyHydrationService(usecase.LazyHydrationServiceDependencies{
		Stubs:         catalogRepo,
		Hydrator:      hydrator,
		FranchiseRepo: syncRepo,
		CatalogRepo:   catalogRepo,
		HydrationSink: syncRepo,
		Cache:         detailsCache,
		Failures:      failureStore,
		Logger:        logger,
	})
	catalogRefresh := usecase.NewCatalogRefreshService(usecase.CatalogRefreshServiceDependencies{
		Catalog:  catalogRepo,
		Hydrator: usecase.NewSyncCatalogHydratorWithFailureStore(malClient, catalogRepo, failureStore, logger),
		Cache:    detailsCache,
		Logger:   logger,
	})

	// Replay any details that a previous run staged in the file cache but never
	// committed to the database. After the refactor this buffer is not populated
	// during normal operation, so this is a one-time migration path for older
	// staged data. It becomes a no-op once the staging file is empty.
	if err := lazyHydration.ReplayStagedDetails(ctx); err != nil {
		logger.Warn("lazy_worker", "cannot replay staged anime details into catalog", "err", err)
	}

	runCycle := func() error {
		resolved, err := lazyHydration.ResolveStubs(ctx, cfg.BatchSize)
		if err != nil {
			return err
		}
		refreshed, err := catalogRefresh.RefreshStaleCatalog(ctx, cfg.TTL, cfg.BatchSize)
		if err != nil {
			return err
		}
		logger.Info("lazy_worker", "lazy worker cycle complete", "stubs_resolved", resolved, "stale_refreshed", refreshed)
		return nil
	}

	if cfg.Once {
		return runCycle()
	}

	logger.Info("lazy_worker", "lazy worker started", "interval", cfg.Interval.String(), "batch", cfg.BatchSize, "ttl", cfg.TTL.String())
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		if err := runCycle(); err != nil && ctx.Err() == nil {
			logger.Error("lazy_worker", "lazy worker cycle failed", "err", err)
		}

		select {
		case <-ctx.Done():
			logger.Info("lazy_worker", "lazy worker stopping")
			return nil
		case <-ticker.C:
		}
	}
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
