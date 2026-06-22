package filecache

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHydrationFailureCachePersistsBackoffAndClearsSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), HydrationFailureCacheName)
	cache := NewHydrationFailureCache(path, nil)
	store, err := cache.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}

	attemptedAt := time.Date(2026, time.June, 22, 12, 0, 0, 0, time.UTC)
	firstRetry, err := store.RecordNotFound(2268, attemptedAt)
	if err != nil {
		t.Fatalf("RecordNotFound() returned error: %v", err)
	}
	if want := attemptedAt.Add(24 * time.Hour); !firstRetry.Equal(want) {
		t.Fatalf("first retry = %v, want %v", firstRetry, want)
	}
	if store.ShouldAttempt(2268, attemptedAt.Add(time.Hour)) {
		t.Fatal("ShouldAttempt() = true during the first backoff")
	}
	if !store.ShouldAttempt(2268, firstRetry) {
		t.Fatal("ShouldAttempt() = false when the first backoff expires")
	}

	secondRetry, err := store.RecordNotFound(2268, firstRetry)
	if err != nil {
		t.Fatalf("second RecordNotFound() returned error: %v", err)
	}
	if want := firstRetry.Add(7 * 24 * time.Hour); !secondRetry.Equal(want) {
		t.Fatalf("second retry = %v, want %v", secondRetry, want)
	}
	thirdRetry, err := store.RecordNotFound(2268, secondRetry)
	if err != nil {
		t.Fatalf("third RecordNotFound() returned error: %v", err)
	}
	if want := secondRetry.Add(30 * 24 * time.Hour); !thirdRetry.Equal(want) {
		t.Fatalf("third retry = %v, want %v", thirdRetry, want)
	}

	reloaded, err := cache.Open(context.Background())
	if err != nil {
		t.Fatalf("reloading cache returned error: %v", err)
	}
	if reloaded.ShouldAttempt(2268, thirdRetry.Add(-time.Second)) {
		t.Fatal("reloaded cache lost the deferred entry")
	}
	if reloaded.DeferredCount(thirdRetry.Add(-time.Second)) != 1 {
		t.Fatalf("DeferredCount() = %d, want 1", reloaded.DeferredCount(thirdRetry.Add(-time.Second)))
	}

	if err := reloaded.MarkSucceeded([]int{2268}); err != nil {
		t.Fatalf("MarkSucceeded() returned error: %v", err)
	}
	if !reloaded.ShouldAttempt(2268, attemptedAt) {
		t.Fatal("successful id remains deferred")
	}
	cleared, err := cache.Open(context.Background())
	if err != nil {
		t.Fatalf("reloading cleared cache returned error: %v", err)
	}
	if !cleared.ShouldAttempt(2268, attemptedAt) {
		t.Fatal("cleared entry reappeared after reloading the cache")
	}
}
