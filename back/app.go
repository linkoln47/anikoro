package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type App struct {
	Config     AppConfig
	DB         *sql.DB
	HTTPClient *http.Client
	Logger     *slog.Logger

	syncStateMu       sync.Mutex
	activeUserSyncIDs map[int64]struct{}
	syncJobsMu        sync.Mutex
	syncJobs          map[string]*SyncJob
}

func NewApp() *App {
	cfg := loadConfig()

	return &App{
		Config: cfg,
		Logger: newLogger(cfg),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		activeUserSyncIDs: make(map[int64]struct{}),
		syncJobs:          make(map[string]*SyncJob),
	}
}

func (a *App) OpenDB() error {
	if a.DB != nil {
		return nil
	}

	db, err := openDB(a.Config)
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

func (a *App) tryBeginUserSync(userID int64) bool {
	a.syncStateMu.Lock()
	defer a.syncStateMu.Unlock()

	if _, exists := a.activeUserSyncIDs[userID]; exists {
		return false
	}

	a.activeUserSyncIDs[userID] = struct{}{}
	return true
}

func (a *App) finishUserSync(userID int64) {
	a.syncStateMu.Lock()
	defer a.syncStateMu.Unlock()
	delete(a.activeUserSyncIDs, userID)
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
