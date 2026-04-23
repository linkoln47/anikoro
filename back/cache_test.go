package main

import (
	"path/filepath"
	"testing"
)

func TestCache_AnimeDetailsCacheStore_FlushPersistsPendingWrites(t *testing.T) {
	app := NewApp()
	app.Config.DetailsCachePath = filepath.Join(t.TempDir(), DetailsCacheName)

	store := newAnimeDetailsCacheStore(app, nil, 1)

	if err := store.StoreResolved(1, AnimeDetailsInfo{ID: 1, MediaType: "tv"}); err != nil {
		t.Fatalf("first StoreResolved returned error: %v", err)
	}
	if err := store.StoreResolved(2, AnimeDetailsInfo{ID: 2, MediaType: "movie"}); err != nil {
		t.Fatalf("second StoreResolved returned error: %v", err)
	}
	if err := store.FlushPending(); err != nil {
		t.Fatalf("FlushPending returned error: %v", err)
	}

	cached, ok := store.Lookup(2)
	if !ok {
		t.Fatal("expected second item to be present in cache")
	}
	if cached.MediaType != "movie" {
		t.Fatalf("cached media type = %q, want %q", cached.MediaType, "movie")
	}

	saved, err := loadJSONFile[map[int]animeDetailsCacheItem](app.Config.DetailsCachePath)
	if err != nil {
		t.Fatalf("loadJSONFile returned error: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("saved item count = %d, want 2", len(saved))
	}
	if saved[1].MediaType != "tv" {
		t.Fatalf("saved media type for id=1 = %q, want %q", saved[1].MediaType, "tv")
	}
	if saved[2].MediaType != "movie" {
		t.Fatalf("saved media type for id=2 = %q, want %q", saved[2].MediaType, "movie")
	}
}
