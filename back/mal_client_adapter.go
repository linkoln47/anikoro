package main

func newMyAnimeListClient(app *App) *MyAnimeListClient {
	return newMyAnimeListClientWithDependencies(app.HTTPClient, app.Config.ClientID, appSyncLogger{app: app})
}

func (a *App) malClient() MALAnimeClient {
	if a.MALAnimeClient != nil {
		return a.MALAnimeClient
	}
	a.MALAnimeClient = newMyAnimeListClient(a)
	return a.MALAnimeClient
}
