package app

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"test/internal/adapters/postgres"
)

type App struct {
	Config     AppConfig
	DB         *sql.DB
	HTTPClient *http.Client
	Logger     *slog.Logger

	AnimeQueries   *AnimeQueryService
	Sync           *SyncService
	MALAnimeClient MALAnimeClient
	DetailsCache   DetailsCache
	SyncJobs       SyncJobStore
	Auth           *AuthService
	SyncGuard      UserSyncGuard
}

func NewApp() *App {
	cfg := loadConfig()

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

	a.MALAnimeClient = newMyAnimeListClient(a)
	a.DetailsCache = newFileDetailsCache(a)
	a.SyncJobs = newInMemorySyncJobStore()
	a.SyncGuard = newInMemoryUserSyncGuard()
	a.Auth = newAuthService(&a.Config, a.HTTPClient, a.authRepository())
	a.AnimeQueries = newAnimeQueryService(newPostgresAnimeRepository(a.DB))
	a.Sync = newSyncService(newSyncServiceDependencies(a))

	return nil
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

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
