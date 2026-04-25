package app

import (
	"log/slog"

	"github.com/gorilla/mux"
)

type HTTPAPI struct {
	config       *AppConfig
	auth         *AuthService
	animeQueries *AnimeQueryService
	sync         *SyncService
	syncJobs     SyncJobStore
	logger       *slog.Logger
}

func newHTTPAPI(app *App) *HTTPAPI {
	return &HTTPAPI{
		config:       &app.Config,
		auth:         app.Auth,
		animeQueries: app.AnimeQueries,
		sync:         app.Sync,
		syncJobs:     app.SyncJobs,
		logger:       app.Logger,
	}
}

func (a *App) SetupRouter() *mux.Router {
	return newHTTPAPI(a).SetupRouter()
}

func (api *HTTPAPI) logInfo(component, msg string, args ...any) {
	api.logger.Info(msg, withComponent(component, args)...)
}

func (api *HTTPAPI) logWarn(component, msg string, args ...any) {
	api.logger.Warn(msg, withComponent(component, args)...)
}

func (api *HTTPAPI) logError(component, msg string, args ...any) {
	api.logger.Error(msg, withComponent(component, args)...)
}
