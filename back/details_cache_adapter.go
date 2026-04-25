package main

import "context"

type FileDetailsCache struct {
	app *App
}

func newFileDetailsCache(app *App) *FileDetailsCache {
	return &FileDetailsCache{app: app}
}

func (a *App) detailsCache() DetailsCache {
	if a.DetailsCache != nil {
		return a.DetailsCache
	}
	a.DetailsCache = newFileDetailsCache(a)
	return a.DetailsCache
}

func (cache *FileDetailsCache) OpenDetailsCache(ctx context.Context) (AnimeDetailsCacheStore, error) {
	ctx = ensureContext(ctx)
	if err := ctx.Err(); err != nil {
		return newAnimeDetailsCacheStore(cache.app, nil, DetailsCacheFlushBatch), err
	}

	items, err := cache.app.loadDetailsCache()
	if err != nil {
		items = map[int]animeDetailsCacheItem{}
	}

	return newAnimeDetailsCacheStore(cache.app, items, DetailsCacheFlushBatch), err
}
