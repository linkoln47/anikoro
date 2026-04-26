package app

import (
	"github.com/gorilla/mux"
	"test/internal/httpapi"
)

func (a *App) SetupRouter() *mux.Router {
	api := httpapi.New(httpapi.Dependencies{
		Config: httpapi.Config{
			ClientID:      a.Config.ClientID,
			ClientSecret:  a.Config.ClientSecret,
			RedirectURI:   a.Config.RedirectURI,
			FrontendURL:   a.Config.FrontendURL,
			SessionSecret: a.Config.SessionSecret,
		},
		Auth:         a.Auth,
		AnimeQueries: a.AnimeQueries,
		Sync:         a.Sync,
		SyncJobs:     a.SyncJobs,
		Logger:       a.Logger,
	})
	return api.SetupRouter()
}
