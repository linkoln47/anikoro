package filecache

import (
	"sync"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	DetailsCacheName       = ".mal_anime_details_cache.json"
	DetailsCacheFlushBatch = 25
)

type animeDetailsCacheItem struct {
	Title           string                 `json:"title"`
	MediaType       string                 `json:"media_type"`
	StartDate       string                 `json:"start_date"`
	StartSeasonYear int                    `json:"start_season_year,omitempty"`
	StartSeasonName string                 `json:"start_season_name,omitempty"`
	ImageMediumURL  string                 `json:"image_medium_url"`
	ImageLargeURL   string                 `json:"image_large_url"`
	NumEpisodes     int                    `json:"num_episodes,omitempty"`
	MalScore        float64                `json:"mal_score,omitempty"`
	Related         []domain.AnimeRelation `json:"related"`
	RelatedIDs      []int                  `json:"related_ids"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Resolved        bool                   `json:"resolved,omitempty"`
}

type animeDetailsCacheStore struct {
	save            func(map[int]animeDetailsCacheItem) error
	mu              sync.Mutex
	flushCond       *sync.Cond
	items           map[int]animeDetailsCacheItem
	dirtyUpdates    int
	flushEvery      int
	flushInProgress bool
}

var _ ports.AnimeDetailsCacheStore = (*animeDetailsCacheStore)(nil)

func (item animeDetailsCacheItem) toInfo() domain.AnimeDetails {
	return domain.AnimeDetails{
		Title:           item.Title,
		MediaType:       item.MediaType,
		StartDate:       item.StartDate,
		StartSeasonYear: item.StartSeasonYear,
		StartSeasonName: item.StartSeasonName,
		ImageMediumURL:  item.ImageMediumURL,
		ImageLargeURL:   item.ImageLargeURL,
		NumEpisodes:     item.NumEpisodes,
		MalScore:        item.MalScore,
		Related:         append([]domain.AnimeRelation(nil), item.Related...),
		RelatedIDs:      append([]int(nil), item.RelatedIDs...),
	}
}

func (item animeDetailsCacheItem) toCachedDetails() ports.CachedAnimeDetails {
	return ports.CachedAnimeDetails{
		Details:   item.toInfo(),
		UpdatedAt: item.UpdatedAt,
		Resolved:  item.Resolved,
	}
}

func newAnimeDetailsCacheStore(save func(map[int]animeDetailsCacheItem) error, items map[int]animeDetailsCacheItem, flushEvery int) *animeDetailsCacheStore {
	if items == nil {
		items = map[int]animeDetailsCacheItem{}
	}
	if flushEvery <= 0 {
		flushEvery = DetailsCacheFlushBatch
	}

	store := &animeDetailsCacheStore{
		save:       save,
		items:      items,
		flushEvery: flushEvery,
	}
	store.flushCond = sync.NewCond(&store.mu)
	return store
}

func (store *animeDetailsCacheStore) StagedDetails() []ports.CachedAnimeDetails {
	store.mu.Lock()
	defer store.mu.Unlock()

	staged := make([]ports.CachedAnimeDetails, 0, len(store.items))
	for animeID, item := range store.items {
		cached := item.toCachedDetails()
		cached.Details.ID = animeID
		staged = append(staged, cached)
	}
	return staged
}

func (store *animeDetailsCacheStore) MarkPersisted(animeIDs []int) error {
	store.mu.Lock()

	removed := 0
	for _, animeID := range animeIDs {
		if _, ok := store.items[animeID]; !ok {
			continue
		}
		delete(store.items, animeID)
		removed++
	}
	if removed == 0 {
		store.mu.Unlock()
		return nil
	}

	store.dirtyUpdates += removed
	snapshot, shouldFlush := store.beginFlushLocked(false)
	store.mu.Unlock()

	if !shouldFlush {
		return nil
	}

	return store.flushSnapshot(snapshot)
}

func (store *animeDetailsCacheStore) StoreResolved(animeID int, details domain.AnimeDetails) error {
	store.mu.Lock()

	store.items[animeID] = animeDetailsCacheItem{
		Title:           details.Title,
		MediaType:       details.MediaType,
		StartDate:       details.StartDate,
		StartSeasonYear: details.StartSeasonYear,
		StartSeasonName: details.StartSeasonName,
		ImageMediumURL:  details.ImageMediumURL,
		ImageLargeURL:   details.ImageLargeURL,
		NumEpisodes:     details.NumEpisodes,
		MalScore:        details.MalScore,
		Related:         append([]domain.AnimeRelation(nil), details.Related...),
		RelatedIDs:      append([]int(nil), details.RelatedIDs...),
		UpdatedAt:       time.Now(),
		Resolved:        true,
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
	err := store.save(snapshot)

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
	item.Related = append([]domain.AnimeRelation(nil), item.Related...)
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
