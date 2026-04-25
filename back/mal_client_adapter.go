package main

import "context"

type MyAnimeListClient struct {
	app *App
}

func newMyAnimeListClient(app *App) *MyAnimeListClient {
	return &MyAnimeListClient{app: app}
}

func (client *MyAnimeListClient) FetchCompletedList(ctx context.Context, token string) ([]CompletedAnimeEntry, error) {
	return client.app.FetchCompletedList(ctx, token)
}

func (client *MyAnimeListClient) FetchPublicCompletedList(ctx context.Context, username string) ([]CompletedAnimeEntry, error) {
	return client.app.FetchPublicCompletedList(ctx, username)
}

func (client *MyAnimeListClient) FetchAnimeDetails(
	ctx context.Context,
	auth MALAuth,
	animeID int,
	cache AnimeDetailsCacheStore,
	mode AnimeDetailsFetchMode,
) (AnimeDetails, error) {
	return client.app.FetchAnimeDetails(ctx, auth, animeID, cache, mode)
}

func (a *App) malClient() MALAnimeClient {
	if a.MALAnimeClient != nil {
		return a.MALAnimeClient
	}
	a.MALAnimeClient = newMyAnimeListClient(a)
	return a.MALAnimeClient
}
