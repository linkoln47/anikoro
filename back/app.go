package main

import (
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

type App struct {
	Config     AppConfig
	DB         *sql.DB
	HTTPClient *http.Client
	Logger     *slog.Logger
}

func NewApp() *App {
	cfg := loadConfig()

	return &App{
		Config: cfg,
		Logger: newLogger(cfg),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
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
