package filecache

import (
	"context"
	"errors"
	"os"
	"strings"

	"test/internal/ports"
)

type FileDetailsCache struct {
	path       string
	flushEvery int
	logger     ports.SyncLogger
}

var _ ports.DetailsCache = (*FileDetailsCache)(nil)

func NewDetailsCache(path string, flushEvery int, logger ports.SyncLogger) *FileDetailsCache {
	if flushEvery <= 0 {
		flushEvery = DetailsCacheFlushBatch
	}
	return &FileDetailsCache{
		path:       strings.TrimSpace(path),
		flushEvery: flushEvery,
		logger:     logger,
	}
}

func (cache *FileDetailsCache) OpenDetailsCache(ctx context.Context) (ports.AnimeDetailsCacheStore, error) {
	ctx = ensureContext(ctx)
	if err := ctx.Err(); err != nil {
		return newAnimeDetailsCacheStore(cache.saveDetailsCache, nil, cache.flushEvery), err
	}

	items, err := cache.loadDetailsCache()
	if err != nil {
		items = map[int]animeDetailsCacheItem{}
	}

	return newAnimeDetailsCacheStore(cache.saveDetailsCache, items, cache.flushEvery), err
}

func (cache *FileDetailsCache) loadDetailsCache() (map[int]animeDetailsCacheItem, error) {
	items, err := loadJSONFile[map[int]animeDetailsCacheItem](cache.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cache.debug("cache", "details cache file not found, a new cache will be created", "path", cache.path)
			return map[int]animeDetailsCacheItem{}, nil
		}
		return nil, err
	}

	cache.debug("cache", "details cache file loaded", "path", cache.path)
	if items == nil {
		items = map[int]animeDetailsCacheItem{}
	}
	return items, nil
}

func (cache *FileDetailsCache) saveDetailsCache(items map[int]animeDetailsCacheItem) error {
	return saveJSONFile(cache.path, 0o644, "Cache file", cache.logger, items)
}

func (cache *FileDetailsCache) debug(component, msg string, args ...any) {
	if cache != nil && cache.logger != nil {
		cache.logger.Debug(component, msg, args...)
	}
}

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
