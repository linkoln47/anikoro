package main

import (
	"errors"
	"os"
	"sync"
	"time"
)

const (
	detailsCacheName       = ".mal_anime_details_cache.json"
	detailsCacheTTL        = 168 * time.Hour
	detailsCacheFlushBatch = 25
)

type animeDetailsCacheItem struct {
	RelatedIDs []int     `json:"related_ids"`
	MediaType  string    `json:"media_type"`
	UpdatedAt  time.Time `json:"updated_at"`
	Resolved   bool      `json:"resolved,omitempty"`
}

type animeDetailsCacheStore struct {
	app          *App
	mu           sync.Mutex
	items        map[int]animeDetailsCacheItem
	dirtyUpdates int
	flushEvery   int
}

func (item animeDetailsCacheItem) isUsable() bool {
	return item.Resolved || item.MediaType != ""
}

func (item animeDetailsCacheItem) isFresh(now time.Time) bool {
	return item.isUsable() && !item.UpdatedAt.IsZero() && now.Sub(item.UpdatedAt) <= detailsCacheTTL
}

func (item animeDetailsCacheItem) toInfo() animeDetailsInfo {
	return animeDetailsInfo{
		RelatedIDs: item.RelatedIDs,
		MediaType:  item.MediaType,
	}
}

func newAnimeDetailsCacheStore(app *App, items map[int]animeDetailsCacheItem, flushEvery int) *animeDetailsCacheStore {
	if items == nil {
		items = map[int]animeDetailsCacheItem{}
	}
	if flushEvery <= 0 {
		flushEvery = detailsCacheFlushBatch
	}

	return &animeDetailsCacheStore{
		app:        app,
		items:      items,
		flushEvery: flushEvery,
	}
}

func (store *animeDetailsCacheStore) Lookup(animeID int) (animeDetailsCacheItem, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()

	item, ok := store.items[animeID]
	if !ok {
		return animeDetailsCacheItem{}, false
	}

	return cloneAnimeDetailsCacheItem(item), true
}

func (store *animeDetailsCacheStore) StoreResolved(animeID int, details animeDetailsInfo) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.items[animeID] = animeDetailsCacheItem{
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

func (store *animeDetailsCacheStore) FlushPending() error {
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

func (store *animeDetailsCacheStore) flushLocked() error {
	return store.app.saveDetailsCache(store.items)
}

func cloneAnimeDetailsCacheItem(item animeDetailsCacheItem) animeDetailsCacheItem {
	item.RelatedIDs = append([]int(nil), item.RelatedIDs...)
	return item
}

func (a *App) loadDetailsCache() (map[int]animeDetailsCacheItem, error) {
	cache, err := loadJSONFile[map[int]animeDetailsCacheItem](a.Config.DetailsCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.logDebug("cache", "details cache file not found, a new cache will be created", "path", a.Config.DetailsCachePath)
			return map[int]animeDetailsCacheItem{}, nil
		}
		return nil, err
	}

	a.logDebug("cache", "details cache file loaded", "path", a.Config.DetailsCachePath)
	if cache == nil {
		cache = map[int]animeDetailsCacheItem{}
	}
	return cache, nil
}

func (a *App) saveDetailsCache(cache map[int]animeDetailsCacheItem) error {
	return a.saveJSONFile(a.Config.DetailsCachePath, 0o644, "Cache file", cache)
}
