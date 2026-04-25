package app

func newMyAnimeListClient(app *App) *MyAnimeListClient {
	return newMyAnimeListClientWithDependencies(app.HTTPClient, app.Config.ClientID, appSyncLogger{app: app})
}
