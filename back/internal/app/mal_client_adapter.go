package app

import "test/internal/adapters/mal"

func newMyAnimeListClient(app *App) MALAnimeClient {
	return mal.NewAnimeClient(app.HTTPClient, app.Config.ClientID, appSyncLogger{app: app})
}
