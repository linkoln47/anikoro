package main

import (
	"encoding/json"
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

var detailsCachePath = appFilePath(detailsCacheName)

type animeDetailsCacheItem struct {
	RelatedIDs []int     `json:"related_ids"`
	MediaType  string    `json:"media_type"`
	UpdatedAt  time.Time `json:"updated_at"`
	Resolved   bool      `json:"resolved,omitempty"`
}

type animeDetailsCacheStore struct {
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

func newAnimeDetailsCacheStore(items map[int]animeDetailsCacheItem, flushEvery int) *animeDetailsCacheStore {
	if items == nil {
		items = map[int]animeDetailsCacheItem{}
	}
	if flushEvery <= 0 {
		flushEvery = detailsCacheFlushBatch
	}

	return &animeDetailsCacheStore{
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
	return saveDetailsCache(store.items)
}

func cloneAnimeDetailsCacheItem(item animeDetailsCacheItem) animeDetailsCacheItem {
	item.RelatedIDs = append([]int(nil), item.RelatedIDs...)
	return item
}

func loadDetailsCache() (map[int]animeDetailsCacheItem, error) {
	b, err := os.ReadFile(detailsCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logDebug("cache", "details cache file not found, a new cache will be created", "path", detailsCachePath)
			return map[int]animeDetailsCacheItem{}, nil
		}
		return nil, err
	}

	logDebug("cache", "details cache file loaded", "path", detailsCachePath)

	var cache map[int]animeDetailsCacheItem
	if err := json.Unmarshal(b, &cache); err != nil {
		return nil, err
	}
	if cache == nil {
		cache = map[int]animeDetailsCacheItem{}
	}
	return cache, nil
}

func saveDetailsCache(cache map[int]animeDetailsCacheItem) error {
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return writeFileWithChangeLog(detailsCachePath, b, 0o644, "Cache file")
}
