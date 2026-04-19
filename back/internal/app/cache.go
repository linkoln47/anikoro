package app

import (
	"errors"
	"os"
	"sync"
	"time"
)

const (
	DetailsCacheName       = ".mal_anime_details_cache.json"
	DetailsCacheTTL        = 168 * time.Hour
	DetailsCacheFlushBatch = 25
)

type AnimeDetailsCacheItem struct {
	RelatedIDs []int     `json:"related_ids"`
	MediaType  string    `json:"media_type"`
	UpdatedAt  time.Time `json:"updated_at"`
	Resolved   bool      `json:"resolved,omitempty"`
}

type AnimeDetailsCacheStore struct {
	app          *App
	mu           sync.Mutex
	items        map[int]AnimeDetailsCacheItem
	dirtyUpdates int
	flushEvery   int
}

func (item AnimeDetailsCacheItem) isUsable() bool {
	return item.Resolved || item.MediaType != ""
}

func (item AnimeDetailsCacheItem) isFresh(now time.Time) bool {
	return item.isUsable() && !item.UpdatedAt.IsZero() && now.Sub(item.UpdatedAt) <= DetailsCacheTTL
}

func (item AnimeDetailsCacheItem) toInfo() AnimeDetailsInfo {
	return AnimeDetailsInfo{
		RelatedIDs: item.RelatedIDs,
		MediaType:  item.MediaType,
	}
}

func NewAnimeDetailsCacheStore(app *App, items map[int]AnimeDetailsCacheItem, flushEvery int) *AnimeDetailsCacheStore {
	if items == nil {
		items = map[int]AnimeDetailsCacheItem{}
	}
	if flushEvery <= 0 {
		flushEvery = DetailsCacheFlushBatch
	}

	return &AnimeDetailsCacheStore{
		app:        app,
		items:      items,
		flushEvery: flushEvery,
	}
}

func (store *AnimeDetailsCacheStore) Lookup(animeID int) (AnimeDetailsCacheItem, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()

	item, ok := store.items[animeID]
	if !ok {
		return AnimeDetailsCacheItem{}, false
	}

	return cloneAnimeDetailsCacheItem(item), true
}

func (store *AnimeDetailsCacheStore) StoreResolved(animeID int, details AnimeDetailsInfo) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.items[animeID] = AnimeDetailsCacheItem{
		RelatedIDs: append([]int(nil), details.RelatedIDs...),
		MediaType:  details.MediaType,
		UpdatedAt:  time.Now(),
		Resolved:   true,
	}
	store.dirtyUpdates++

	if store.dirtyUpdates < store.flushEvery {
		return nil
	}

	if err := store.flushLocked(); err != nil {
		return err
	}

	store.dirtyUpdates = 0
	return nil
}

func (store *AnimeDetailsCacheStore) FlushPending() error {
	store.mu.Lock()
	defer store.mu.Unlock()

	if store.dirtyUpdates == 0 {
		return nil
	}

	if err := store.flushLocked(); err != nil {
		return err
	}

	store.dirtyUpdates = 0
	return nil
}

func (store *AnimeDetailsCacheStore) flushLocked() error {
	return store.app.saveDetailsCache(store.items)
}

func cloneAnimeDetailsCacheItem(item AnimeDetailsCacheItem) AnimeDetailsCacheItem {
	item.RelatedIDs = append([]int(nil), item.RelatedIDs...)
	return item
}

func (a *App) loadDetailsCache() (map[int]AnimeDetailsCacheItem, error) {
	cache, err := loadJSONFile[map[int]AnimeDetailsCacheItem](a.Config.DetailsCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.logDebug("cache", "details cache file not found, a new cache will be created", "path", a.Config.DetailsCachePath)
			return map[int]AnimeDetailsCacheItem{}, nil
		}
		return nil, err
	}

	a.logDebug("cache", "details cache file loaded", "path", a.Config.DetailsCachePath)
	if cache == nil {
		cache = map[int]AnimeDetailsCacheItem{}
	}
	return cache, nil
}

func (a *App) saveDetailsCache(cache map[int]AnimeDetailsCacheItem) error {
	return a.saveJSONFile(a.Config.DetailsCachePath, 0o644, "Cache file", cache)
}
