package main

import (
	"errors"
	"os"
	"sync"
	"time"
)

const (
	DetailsCacheName       = ".mal_anime_details_cache.json"
	DetailsCacheFlushBatch = 25
)

type animeDetailsCacheItem struct {
	Title          string          `json:"title"`
	MediaType      string          `json:"media_type"`
	StartDate      string          `json:"start_date"`
	ImageMediumURL string          `json:"image_medium_url"`
	ImageLargeURL  string          `json:"image_large_url"`
	Related        []AnimeRelation `json:"related"`
	RelatedIDs     []int           `json:"related_ids"`
	UpdatedAt      time.Time       `json:"updated_at"`
	Resolved       bool            `json:"resolved,omitempty"`
}

type animeDetailsCacheStore struct {
	app             *App
	mu              sync.Mutex
	flushCond       *sync.Cond
	items           map[int]animeDetailsCacheItem
	dirtyUpdates    int
	flushEvery      int
	flushInProgress bool
}

func (item animeDetailsCacheItem) toInfo() AnimeDetails {
	return AnimeDetails{
		Title:          item.Title,
		MediaType:      item.MediaType,
		StartDate:      item.StartDate,
		ImageMediumURL: item.ImageMediumURL,
		ImageLargeURL:  item.ImageLargeURL,
		Related:        append([]AnimeRelation(nil), item.Related...),
		RelatedIDs:     append([]int(nil), item.RelatedIDs...),
	}
}

func (item animeDetailsCacheItem) toCachedDetails() CachedAnimeDetails {
	return CachedAnimeDetails{
		Details:   item.toInfo(),
		UpdatedAt: item.UpdatedAt,
		Resolved:  item.Resolved,
	}
}

func newAnimeDetailsCacheStore(app *App, items map[int]animeDetailsCacheItem, flushEvery int) *animeDetailsCacheStore {
	if items == nil {
		items = map[int]animeDetailsCacheItem{}
	}
	if flushEvery <= 0 {
		flushEvery = DetailsCacheFlushBatch
	}

	store := &animeDetailsCacheStore{
		app:        app,
		items:      items,
		flushEvery: flushEvery,
	}
	store.flushCond = sync.NewCond(&store.mu)
	return store
}

func (store *animeDetailsCacheStore) Lookup(animeID int) (CachedAnimeDetails, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()

	item, ok := store.items[animeID]
	if !ok {
		return CachedAnimeDetails{}, false
	}

	return item.toCachedDetails(), true
}

func (store *animeDetailsCacheStore) StoreResolved(animeID int, details AnimeDetails) error {
	store.mu.Lock()

	store.items[animeID] = animeDetailsCacheItem{
		Title:          details.Title,
		MediaType:      details.MediaType,
		StartDate:      details.StartDate,
		ImageMediumURL: details.ImageMediumURL,
		ImageLargeURL:  details.ImageLargeURL,
		Related:        append([]AnimeRelation(nil), details.Related...),
		RelatedIDs:     append([]int(nil), details.RelatedIDs...),
		UpdatedAt:      time.Now(),
		Resolved:       true,
	}
	store.dirtyUpdates++

	snapshot, shouldFlush := store.beginFlushLocked(false)
	store.mu.Unlock()

	if !shouldFlush {
		return nil
	}

	return store.flushSnapshot(snapshot)
}

func (store *animeDetailsCacheStore) FlushPending() error {
	for {
		store.mu.Lock()
		for store.flushInProgress {
			store.flushCond.Wait()
		}

		snapshot, shouldFlush := store.beginFlushLocked(true)
		store.mu.Unlock()
		if !shouldFlush {
			return nil
		}

		if err := store.flushSnapshot(snapshot); err != nil {
			return err
		}
	}
}

func (store *animeDetailsCacheStore) beginFlushLocked(force bool) (map[int]animeDetailsCacheItem, bool) {
	if store.flushInProgress {
		return nil, false
	}
	if store.dirtyUpdates == 0 {
		return nil, false
	}
	if !force && store.dirtyUpdates < store.flushEvery {
		return nil, false
	}

	snapshot := cloneAnimeDetailsCacheItemsMap(store.items)
	store.dirtyUpdates = 0
	store.flushInProgress = true
	return snapshot, true
}

func (store *animeDetailsCacheStore) flushSnapshot(snapshot map[int]animeDetailsCacheItem) error {
	err := store.app.saveDetailsCache(snapshot)

	store.mu.Lock()
	defer store.mu.Unlock()

	store.flushInProgress = false
	if err != nil {
		// Force a later retry with the current in-memory state.
		store.dirtyUpdates++
	}
	store.flushCond.Broadcast()
	return err
}

func cloneAnimeDetailsCacheItem(item animeDetailsCacheItem) animeDetailsCacheItem {
	item.Related = append([]AnimeRelation(nil), item.Related...)
	item.RelatedIDs = append([]int(nil), item.RelatedIDs...)
	return item
}

func cloneAnimeDetailsCacheItemsMap(items map[int]animeDetailsCacheItem) map[int]animeDetailsCacheItem {
	cloned := make(map[int]animeDetailsCacheItem, len(items))
	for animeID, item := range items {
		cloned[animeID] = cloneAnimeDetailsCacheItem(item)
	}
	return cloned
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
