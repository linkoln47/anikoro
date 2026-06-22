package filecache

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"test/internal/ports"
)

const HydrationFailureCacheName = ".mal_anime_hydration_failures_cache.json"

const (
	firstNotFoundBackoff    = 24 * time.Hour
	secondNotFoundBackoff   = 7 * 24 * time.Hour
	repeatedNotFoundBackoff = 30 * 24 * time.Hour
)

type hydrationFailureCacheItem struct {
	StatusCode    int       `json:"status_code"`
	Attempts      int       `json:"attempts"`
	LastAttemptAt time.Time `json:"last_attempt_at"`
	RetryAfter    time.Time `json:"retry_after"`
}

type FileHydrationFailureCache struct {
	path   string
	logger ports.SyncLogger
}

func NewHydrationFailureCache(path string, logger ports.SyncLogger) *FileHydrationFailureCache {
	return &FileHydrationFailureCache{
		path:   strings.TrimSpace(path),
		logger: logger,
	}
}

func (cache *FileHydrationFailureCache) Open(ctx context.Context) (ports.AnimeHydrationFailureStore, error) {
	ctx = ensureContext(ctx)
	if err := ctx.Err(); err != nil {
		return newHydrationFailureStore(cache.save, nil), err
	}

	items, err := loadJSONFile[map[int]hydrationFailureCacheItem](cache.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			items = map[int]hydrationFailureCacheItem{}
			err = nil
		} else {
			items = map[int]hydrationFailureCacheItem{}
		}
	}

	return newHydrationFailureStore(cache.save, items), err
}

func (cache *FileHydrationFailureCache) save(items map[int]hydrationFailureCacheItem) error {
	return saveJSONFile(cache.path, 0o644, "Hydration failure cache", cache.logger, items)
}

type hydrationFailureStore struct {
	save  func(map[int]hydrationFailureCacheItem) error
	mu    sync.RWMutex
	items map[int]hydrationFailureCacheItem
}

var _ ports.AnimeHydrationFailureStore = (*hydrationFailureStore)(nil)

func newHydrationFailureStore(save func(map[int]hydrationFailureCacheItem) error, items map[int]hydrationFailureCacheItem) *hydrationFailureStore {
	if items == nil {
		items = map[int]hydrationFailureCacheItem{}
	}
	return &hydrationFailureStore{save: save, items: items}
}

func (store *hydrationFailureStore) ShouldAttempt(animeID int, now time.Time) bool {
	store.mu.RLock()
	defer store.mu.RUnlock()

	item, ok := store.items[animeID]
	return !ok || item.RetryAfter.IsZero() || !now.Before(item.RetryAfter)
}

func (store *hydrationFailureStore) DeferredCount(now time.Time) int {
	store.mu.RLock()
	defer store.mu.RUnlock()

	count := 0
	for _, item := range store.items {
		if !item.RetryAfter.IsZero() && now.Before(item.RetryAfter) {
			count++
		}
	}
	return count
}

func (store *hydrationFailureStore) RecordNotFound(animeID int, attemptedAt time.Time) (time.Time, error) {
	if animeID <= 0 {
		return time.Time{}, nil
	}
	if attemptedAt.IsZero() {
		attemptedAt = time.Now().UTC()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	item := store.items[animeID]
	item.StatusCode = 404
	item.Attempts++
	item.LastAttemptAt = attemptedAt
	item.RetryAfter = attemptedAt.Add(notFoundBackoff(item.Attempts))
	store.items[animeID] = item

	return item.RetryAfter, store.save(cloneHydrationFailureItems(store.items))
}

func (store *hydrationFailureStore) MarkSucceeded(animeIDs []int) error {
	store.mu.Lock()
	defer store.mu.Unlock()

	changed := false
	for _, animeID := range animeIDs {
		if _, ok := store.items[animeID]; !ok {
			continue
		}
		delete(store.items, animeID)
		changed = true
	}
	if !changed {
		return nil
	}

	return store.save(cloneHydrationFailureItems(store.items))
}

func notFoundBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return firstNotFoundBackoff
	case 2:
		return secondNotFoundBackoff
	default:
		return repeatedNotFoundBackoff
	}
}

func cloneHydrationFailureItems(items map[int]hydrationFailureCacheItem) map[int]hydrationFailureCacheItem {
	cloned := make(map[int]hydrationFailureCacheItem, len(items))
	for animeID, item := range items {
		cloned[animeID] = item
	}
	return cloned
}
