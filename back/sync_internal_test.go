package main

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSyncInternal_BuildUserGroupsFromObservedComponents_MergesOverlappingMemberships(t *testing.T) {
	entries := []AnimeEntry{
		{ID: 2, Title: "Season Two", Score: 8, NumEpisodesWatched: 12},
		{ID: 27, Title: "Season Finale", Score: 10, NumEpisodesWatched: 12},
		{ID: 1, Title: "Season One", Score: 9, NumEpisodesWatched: 12},
	}

	observedMemberSets := []map[int]struct{}{
		{1: {}, 2: {}},
		{1: {}, 2: {}, 27: {}},
		{1: {}, 27: {}},
	}

	seriesGroups, movieGroups, err := buildUserGroupsFromObservedComponents(
		entries,
		observedMemberSets,
		func(animeID int) (string, error) {
			switch animeID {
			case 1, 2, 27:
				return "tv", nil
			default:
				return "", fmt.Errorf("unexpected anime id %d", animeID)
			}
		},
	)
	if err != nil {
		t.Fatalf("buildUserGroupsFromObservedComponents returned error: %v", err)
	}

	if len(movieGroups) != 0 {
		t.Fatalf("movie groups = %#v, want none", movieGroups)
	}

	wantSeries := []GroupedView{
		{
			ID:                 1,
			GroupKey:           "1:2:27",
			DisplayTitle:       "Season Two",
			MergedTitles:       3,
			AvgScore:           9,
			GroupMemberIDs:     []int{1, 2, 27},
			WatchedEpisodesSum: 36,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}
}

func TestSyncInternal_BuildUserGroupsFromObservedComponents_IgnoresUnscoredEntriesInAverage(t *testing.T) {
	entries := []AnimeEntry{
		{ID: 1, Title: "Scored Season", Score: 8, NumEpisodesWatched: 12},
		{ID: 2, Title: "Unscored Season", Score: 0, NumEpisodesWatched: 12},
	}

	seriesGroups, movieGroups, err := buildUserGroupsFromObservedComponents(
		entries,
		[]map[int]struct{}{{1: {}, 2: {}}},
		func(animeID int) (string, error) {
			return "tv", nil
		},
	)
	if err != nil {
		t.Fatalf("buildUserGroupsFromObservedComponents returned error: %v", err)
	}

	if len(movieGroups) != 0 {
		t.Fatalf("movie groups = %#v, want none", movieGroups)
	}

	wantSeries := []GroupedView{
		{
			ID:                 1,
			GroupKey:           "1:2",
			DisplayTitle:       "Scored Season",
			MergedTitles:       2,
			AvgScore:           8,
			GroupMemberIDs:     []int{1, 2},
			WatchedEpisodesSum: 24,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}
}

func TestSyncInternal_BuildUserGroupsFromCatalogWithContext_SplitsStandaloneMovie(t *testing.T) {
	app, mock := newInternalTestApp(t)

	expectUndirectedAnimeRelationIDs(mock, 30, 999)
	expectUndirectedAnimeRelationIDs(mock, 999, 30)
	expectUndirectedAnimeRelationIDs(mock, 20, 10)
	expectUndirectedAnimeRelationIDs(mock, 10, 20)
	expectUndirectedAnimeRelationIDs(mock, 10, 20)
	expectUndirectedAnimeRelationIDs(mock, 20, 10)
	expectAnimeCatalogMediaType(mock, 30, "movie")
	expectAnimeCatalogMediaType(mock, 20, "tv")
	expectAnimeCatalogMediaType(mock, 10, "tv")

	entries := []AnimeEntry{
		{ID: 30, Title: "Standalone Movie", Score: 10, NumEpisodesWatched: 1},
		{ID: 20, Title: "Series Season 2", Score: 8, NumEpisodesWatched: 12},
		{ID: 10, Title: "Series Season 1", Score: 9, NumEpisodesWatched: 12},
	}

	seriesGroups, movieGroups, err := app.buildUserGroupsFromCatalogWithContext(context.Background(), entries)
	if err != nil {
		t.Fatalf("buildUserGroupsFromCatalogWithContext returned error: %v", err)
	}

	wantSeries := []GroupedView{
		{
			ID:                 10,
			GroupKey:           "10:20",
			DisplayTitle:       "Series Season 2",
			MergedTitles:       2,
			AvgScore:           8.5,
			GroupMemberIDs:     []int{10, 20},
			WatchedEpisodesSum: 24,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}

	wantMovies := []GroupedView{
		{
			ID:                 30,
			GroupKey:           "30",
			DisplayTitle:       "Standalone Movie",
			MergedTitles:       1,
			AvgScore:           10,
			GroupMemberIDs:     []int{30},
			WatchedEpisodesSum: 1,
		},
	}
	if !reflect.DeepEqual(movieGroups, wantMovies) {
		t.Fatalf("movie groups mismatch:\n got: %#v\nwant: %#v", movieGroups, wantMovies)
	}
}

func TestSyncInternal_BuildUserGroupsFromCatalogWithContext_KeepsLinkedMovieInSeries(t *testing.T) {
	app, mock := newInternalTestApp(t)

	expectUndirectedAnimeRelationIDs(mock, 100, 200)
	expectUndirectedAnimeRelationIDs(mock, 200, 100)
	expectUndirectedAnimeRelationIDs(mock, 200, 100)
	expectUndirectedAnimeRelationIDs(mock, 100, 200)
	expectAnimeCatalogMediaType(mock, 100, "movie")
	expectAnimeCatalogMediaType(mock, 200, "tv")

	entries := []AnimeEntry{
		{ID: 100, Title: "Movie Part", Score: 7, NumEpisodesWatched: 1},
		{ID: 200, Title: "TV Follow-up", Score: 8, NumEpisodesWatched: 3},
	}

	seriesGroups, movieGroups, err := app.buildUserGroupsFromCatalogWithContext(context.Background(), entries)
	if err != nil {
		t.Fatalf("buildUserGroupsFromCatalogWithContext returned error: %v", err)
	}

	if len(movieGroups) != 0 {
		t.Fatalf("expected no movie groups, got %#v", movieGroups)
	}

	wantSeries := []GroupedView{
		{
			ID:                 100,
			GroupKey:           "100:200",
			DisplayTitle:       "Movie Part",
			MergedTitles:       2,
			AvgScore:           7.5,
			GroupMemberIDs:     []int{100, 200},
			WatchedEpisodesSum: 4,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}
}

func TestSyncInternal_ResolveAnimeCatalogBatchWithContext_StartsRetryWhileAnotherPrimaryStillRunning(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 1, 2)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{ID: 1, MediaType: "movie"}})
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{ID: 2, MediaType: "tv"}})

	slowPrimaryRelease := make(chan struct{})
	retryStarted := make(chan struct{}, 1)
	var callsMu sync.Mutex
	callCounts := map[int]int{}
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)

			callsMu.Lock()
			callCounts[animeID]++
			callCount := callCounts[animeID]
			callsMu.Unlock()

			switch animeID {
			case 1:
				if callCount == 1 {
					return internalTextHTTPResponse(400, "primary lookup failed"), nil
				}
				select {
				case retryStarted <- struct{}{}:
				default:
				}
				return internalJSONHTTPResponse(200, animeDetailsJSON(1, "movie")), nil
			case 2:
				if callCount != 1 {
					return nil, fmt.Errorf("unexpected call %d for anime id %d", callCount, animeID)
				}
				<-slowPrimaryRelease
				return internalJSONHTTPResponse(200, animeDetailsJSON(2, "tv")), nil
			default:
				return nil, fmt.Errorf("unexpected anime id %d", animeID)
			}
		},
	}

	type batchResult struct {
		results []animeCatalogHydrationResult
		err     error
	}
	done := make(chan batchResult, 1)
	go func() {
		results, err := app.resolveAnimeCatalogBatchWithContext(context.Background(), "token", []int{1, 2}, cache)
		done <- batchResult{results: results, err: err}
	}()

	select {
	case <-retryStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("retry did not start while another primary request was still running")
	}

	time.Sleep(animeCatalogPersistWindow * 2)
	close(slowPrimaryRelease)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("resolveAnimeCatalogBatchWithContext returned error: %v", got.err)
		}
		if len(got.results) != 2 {
			t.Fatalf("result count = %d, want 2", len(got.results))
		}
		if got.results[0].AnimeID != 1 || got.results[1].AnimeID != 2 {
			t.Fatalf("results = %#v, want anime ids [1 2]", got.results)
		}
	case <-time.After(time.Second):
		t.Fatal("resolveAnimeCatalogBatchWithContext did not finish after releasing slow primary")
	}
}

func TestSyncInternal_ResolveAnimeCatalogBatchWithContext_UsesRetryAndCachesResult(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 7)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{ID: 7, MediaType: "movie"}})

	requests := 0
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)
			if animeID != 7 {
				return nil, fmt.Errorf("unexpected anime id %d", animeID)
			}

			requests++
			if requests == 1 {
				return internalTextHTTPResponse(500, "primary lookup failed"), nil
			}
			return internalJSONHTTPResponse(200, animeDetailsJSON(7, "movie")), nil
		},
	}

	results, err := app.resolveAnimeCatalogBatchWithContext(context.Background(), "token", []int{7}, cache)
	if err != nil {
		t.Fatalf("resolveAnimeCatalogBatchWithContext returned error: %v", err)
	}

	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}
	if len(results) != 1 || results[0].AnimeID != 7 {
		t.Fatalf("results = %#v, want one result for id=7", results)
	}

	cached, ok := cache.Lookup(7)
	if !ok {
		t.Fatal("expected retry result to be cached")
	}
	if !cached.Resolved {
		t.Fatal("expected cached retry result to be marked resolved")
	}
	if cached.MediaType != "movie" {
		t.Fatalf("cached media type = %q, want %q", cached.MediaType, "movie")
	}
}

func TestSyncInternal_ResolveAnimeCatalogBatchWithContext_PreflightsFreshIDsInBatch(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	freshSyncedAt := time.Now().Add(-time.Minute)
	staleSyncedAt := time.Now().Add(-DetailsCacheTTL - time.Minute)
	expectAnimeCatalogStatesByIDs(
		mock,
		internalSQLRows("anime_id", "resolved", "details_synced_at").
			AddRow(1, true, freshSyncedAt).
			AddRow(2, true, staleSyncedAt),
		1, 2, 3,
	)
	expectAnimeRelationIDsBySourceIDs(
		mock,
		internalSQLRows("anime_id", "related_anime_id").
			AddRow(1, 10).
			AddRow(1, 11),
		1,
	)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{
		{ID: 2, MediaType: "tv", RelatedIDs: []int{20}},
		{ID: 3, MediaType: "movie", RelatedIDs: []int{30}},
	})

	started := make(chan int, 2)
	releaseFetch := make(chan struct{})
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)
			switch animeID {
			case 2:
				started <- animeID
				<-releaseFetch
				return internalJSONHTTPResponse(200, animeDetailsJSON(2, "tv", 20)), nil
			case 3:
				started <- animeID
				<-releaseFetch
				return internalJSONHTTPResponse(200, animeDetailsJSON(3, "movie", 30)), nil
			default:
				return nil, fmt.Errorf("unexpected anime id %d", animeID)
			}
		},
	}

	type batchResult struct {
		results []animeCatalogHydrationResult
		err     error
	}
	done := make(chan batchResult, 1)
	go func() {
		results, err := app.resolveAnimeCatalogBatchWithContext(context.Background(), "token", []int{1, 2, 3}, cache)
		done <- batchResult{results: results, err: err}
	}()

	seen := make([]int, 0, 2)
	for len(seen) < 2 {
		select {
		case animeID := <-started:
			seen = append(seen, animeID)
		case <-time.After(300 * time.Millisecond):
			t.Fatal("did not observe both stale ids start fetching")
		}
	}
	sort.Ints(seen)
	if !reflect.DeepEqual(seen, []int{2, 3}) {
		t.Fatalf("started fetch ids = %#v, want [2 3]", seen)
	}

	close(releaseFetch)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("resolveAnimeCatalogBatchWithContext returned error: %v", got.err)
		}

		wantResults := []animeCatalogHydrationResult{
			{AnimeID: 1, RelatedIDs: []int{10, 11}},
			{AnimeID: 2, RelatedIDs: []int{20}},
			{AnimeID: 3, RelatedIDs: []int{30}},
		}
		if !reflect.DeepEqual(got.results, wantResults) {
			t.Fatalf("results mismatch:\n got: %#v\nwant: %#v", got.results, wantResults)
		}
	case <-time.After(time.Second):
		t.Fatal("resolveAnimeCatalogBatchWithContext did not finish after releasing fetches")
	}
}

func TestSyncInternal_ResolveAnimeCatalogBatchWithContext_BatchesPersistedDetails(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 1, 2)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{
		{ID: 1, MediaType: "tv", RelatedIDs: []int{101}},
		{ID: 2, MediaType: "tv", RelatedIDs: []int{102}},
	})

	releaseFetch := make(chan struct{})
	startedFetches := make(chan int, 2)
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)
			select {
			case startedFetches <- animeID:
			default:
			}
			<-releaseFetch
			return internalJSONHTTPResponse(200, animeDetailsJSON(animeID, "tv", animeID+100)), nil
		},
	}

	done := make(chan struct {
		results []animeCatalogHydrationResult
		err     error
	}, 1)
	go func() {
		results, err := app.resolveAnimeCatalogBatchWithContext(context.Background(), "token", []int{1, 2}, cache)
		done <- struct {
			results []animeCatalogHydrationResult
			err     error
		}{results: results, err: err}
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-startedFetches:
		case <-time.After(300 * time.Millisecond):
			t.Fatal("did not observe both primary fetches start")
		}
	}

	close(releaseFetch)

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("resolveAnimeCatalogBatchWithContext returned error: %v", got.err)
		}

		wantResults := []animeCatalogHydrationResult{
			{AnimeID: 1, RelatedIDs: []int{101}},
			{AnimeID: 2, RelatedIDs: []int{102}},
		}
		if !reflect.DeepEqual(got.results, wantResults) {
			t.Fatalf("results mismatch:\n got: %#v\nwant: %#v", got.results, wantResults)
		}
	case <-time.After(time.Second):
		t.Fatal("resolveAnimeCatalogBatchWithContext did not finish after releasing fetches")
	}
}

func TestSyncInternal_ResolveAnimeCatalogBatchWithContext_IgnoresCharacterAndOtherRelationsInTraversal(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 1)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{
		ID:        1,
		Title:     "Traversal Root",
		MediaType: "tv",
		Related: []AnimeRelationInfo{
			{ID: 10, Title: "Canon sequel", RelationType: "sequel", RelationTypeFormatted: "Sequel"},
			{ID: 20, Title: "Promo crossover", RelationType: "other", RelationTypeFormatted: "Other"},
			{ID: 30, Title: "Ad campaign", RelationType: "character", RelationTypeFormatted: "Character"},
		},
	}})

	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)
			if animeID != 1 {
				return nil, fmt.Errorf("unexpected anime id %d", animeID)
			}

			return internalJSONHTTPResponse(200, `{
				"id":1,
				"title":"Traversal Root",
				"media_type":"tv",
				"related_anime":[
					{"node":{"id":10,"title":"Canon sequel"},"relation_type":"sequel","relation_type_formatted":"Sequel"},
					{"node":{"id":20,"title":"Promo crossover"},"relation_type":"other","relation_type_formatted":"Other"},
					{"node":{"id":30,"title":"Ad campaign"},"relation_type":"character","relation_type_formatted":"Character"}
				]
			}`), nil
		},
	}

	results, err := app.resolveAnimeCatalogBatchWithContext(context.Background(), "token", []int{1}, cache)
	if err != nil {
		t.Fatalf("resolveAnimeCatalogBatchWithContext returned error: %v", err)
	}

	want := []animeCatalogHydrationResult{{
		AnimeID:    1,
		RelatedIDs: []int{10},
	}}
	if !reflect.DeepEqual(results, want) {
		t.Fatalf("results mismatch:\n got: %#v\nwant: %#v", results, want)
	}
}

func TestSyncInternal_ResolveAnimeCatalogBatchWithContext_ReturnsSummarizedRetryErrors(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 1, 2, 3, 4)

	var callsMu sync.Mutex
	callCounts := map[int]int{}
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)

			callsMu.Lock()
			callCounts[animeID]++
			callCount := callCounts[animeID]
			callsMu.Unlock()

			if callCount == 1 {
				return internalTextHTTPResponse(500, "primary lookup failed"), nil
			}
			return internalTextHTTPResponse(400, "retry lookup failed"), nil
		},
	}

	_, err := app.resolveAnimeCatalogBatchWithContext(context.Background(), "token", []int{1, 2, 3, 4}, cache)
	if err == nil {
		t.Fatal("expected retry failure error, got nil")
	}

	message := err.Error()
	if !strings.Contains(message, "failed to resolve anime details after retry for 4 catalog ids") {
		t.Fatalf("error %q does not mention retry failure count", message)
	}
	if !strings.Contains(message, "retry lookup failed") {
		t.Fatalf("error %q does not include retry failure details", message)
	}
	if !strings.Contains(message, "and 1 more") {
		t.Fatalf("error %q does not include summarized tail", message)
	}
}

func TestSyncInternal_HydrateCatalogGraphWithContext_ProcessesIndependentSeedsConcurrently(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	mock.MatchExpectationsInOrder(false)
	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 1)
	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 2)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{ID: 2, MediaType: "tv"}})
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{ID: 1, MediaType: "tv"}})

	slowSeedRelease := make(chan struct{})
	fastSeedStarted := make(chan struct{}, 1)
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)
			switch animeID {
			case 1:
				<-slowSeedRelease
				return internalJSONHTTPResponse(200, animeDetailsJSON(1, "tv")), nil
			case 2:
				select {
				case fastSeedStarted <- struct{}{}:
				default:
				}
				return internalJSONHTTPResponse(200, animeDetailsJSON(2, "tv")), nil
			default:
				return internalJSONHTTPResponse(200, animeDetailsJSON(animeID, "tv")), nil
			}
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- app.hydrateCatalogGraphWithContext(context.Background(), "token", []int{1, 2}, cache)
	}()

	select {
	case <-fastSeedStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("independent seed did not start while another franchise was still blocked")
	}

	time.Sleep(animeCatalogPersistWindow * 2)
	close(slowSeedRelease)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hydrateCatalogGraphWithContext returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("hydrateCatalogGraphWithContext did not finish after releasing blocked seed")
	}
}

func TestSyncInternal_HydrateCatalogGraphWithContext_DeduplicatesSharedNodesAcrossParallelSeeds(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	mock.MatchExpectationsInOrder(false)
	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 1)
	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 2)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{
		{ID: 1, MediaType: "tv", RelatedIDs: []int{100}},
		{ID: 2, MediaType: "tv", RelatedIDs: []int{100}},
	})
	expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), 100)
	expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{{ID: 100, MediaType: "tv"}})

	seedStarted := make(chan int, 2)
	releaseSeeds := make(chan struct{})
	var (
		mu         sync.Mutex
		fetchCount = map[int]int{}
	)
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)

			mu.Lock()
			fetchCount[animeID]++
			mu.Unlock()

			switch animeID {
			case 1, 2:
				select {
				case seedStarted <- animeID:
				default:
				}
				<-releaseSeeds
				return internalJSONHTTPResponse(200, animeDetailsJSON(animeID, "tv", 100)), nil
			case 100:
				return internalJSONHTTPResponse(200, animeDetailsJSON(100, "tv")), nil
			default:
				return internalJSONHTTPResponse(200, animeDetailsJSON(animeID, "tv")), nil
			}
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- app.hydrateCatalogGraphWithContext(context.Background(), "token", []int{1, 2}, cache)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-seedStarted:
		case <-time.After(300 * time.Millisecond):
			t.Fatal("did not observe both seed franchises start")
		}
	}

	close(releaseSeeds)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hydrateCatalogGraphWithContext returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("hydrateCatalogGraphWithContext did not finish after releasing seed roots")
	}

	mu.Lock()
	defer mu.Unlock()
	if fetchCount[100] != 1 {
		t.Fatalf("shared anime fetch count = %d, want 1", fetchCount[100])
	}
}

func TestSyncInternal_HydrateSingleFranchiseWithContext_RespectsNodeCap(t *testing.T) {
	app, mock := newInternalTestApp(t)
	cache := newAnimeDetailsCacheStore(app, nil, 1000)

	for animeID := 1; animeID <= maxNodesPerFranchise; animeID++ {
		expectAnimeCatalogStatesByIDs(mock, internalSQLRows("anime_id", "resolved", "details_synced_at"), animeID)

		relatedIDs := []int(nil)
		if animeID < maxNodesPerFranchise+5 {
			relatedIDs = []int{animeID + 1}
		}
		expectSaveAnimeCatalogDetailsBatch(t, mock, []AnimeDetailsInfo{
			{ID: animeID, MediaType: "tv", RelatedIDs: relatedIDs},
		})
	}

	requestCount := 0
	maxRequestedID := 0
	app.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			animeID := animeIDFromRequest(t, req)
			if animeID > maxNodesPerFranchise {
				return nil, fmt.Errorf("unexpected anime id %d beyond node cap", animeID)
			}

			requestCount++
			if animeID > maxRequestedID {
				maxRequestedID = animeID
			}

			if animeID < maxNodesPerFranchise+5 {
				return internalJSONHTTPResponse(200, animeDetailsJSON(animeID, "tv", animeID+1)), nil
			}
			return internalJSONHTTPResponse(200, animeDetailsJSON(animeID, "tv")), nil
		},
	}

	if err := app.hydrateSingleFranchiseWithContext(context.Background(), "token", 1, cache); err != nil {
		t.Fatalf("hydrateSingleFranchiseWithContext returned error: %v", err)
	}

	if requestCount != maxNodesPerFranchise {
		t.Fatalf("request count = %d, want %d", requestCount, maxNodesPerFranchise)
	}
	if maxRequestedID != maxNodesPerFranchise {
		t.Fatalf("max requested anime id = %d, want %d", maxRequestedID, maxNodesPerFranchise)
	}
}

func TestSyncInternal_BuildUserGroupsFromCatalogWithContext_DeduplicatesDuplicateEntries(t *testing.T) {
	app, mock := newInternalTestApp(t)

	expectUndirectedAnimeRelationIDs(mock, 2, 1, 27)
	expectUndirectedAnimeRelationIDs(mock, 1, 2, 27)
	expectUndirectedAnimeRelationIDs(mock, 27, 1, 2)
	expectUndirectedAnimeRelationIDs(mock, 27, 1, 2)
	expectUndirectedAnimeRelationIDs(mock, 1, 2, 27)
	expectUndirectedAnimeRelationIDs(mock, 2, 1, 27)
	expectUndirectedAnimeRelationIDs(mock, 1, 2, 27)
	expectUndirectedAnimeRelationIDs(mock, 2, 1, 27)
	expectUndirectedAnimeRelationIDs(mock, 27, 1, 2)
	expectAnimeCatalogMediaType(mock, 2, "tv")
	expectAnimeCatalogMediaType(mock, 27, "tv")
	expectAnimeCatalogMediaType(mock, 1, "tv")

	entries := []AnimeEntry{
		{ID: 2, Title: "Season Two", Score: 8, NumEpisodesWatched: 12},
		{ID: 27, Title: "Season Finale", Score: 10, NumEpisodesWatched: 12},
		{ID: 1, Title: "Season One", Score: 9, NumEpisodesWatched: 12},
		{ID: 1, Title: "Season One", Score: 9, NumEpisodesWatched: 12},
	}

	seriesGroups, movieGroups, err := app.buildUserGroupsFromCatalogWithContext(context.Background(), entries)
	if err != nil {
		t.Fatalf("buildUserGroupsFromCatalogWithContext returned error: %v", err)
	}

	if len(movieGroups) != 0 {
		t.Fatalf("movie groups = %#v, want none", movieGroups)
	}

	wantSeries := []GroupedView{
		{
			ID:                 1,
			GroupKey:           "1:2:27",
			DisplayTitle:       "Season Two",
			MergedTitles:       3,
			AvgScore:           9,
			GroupMemberIDs:     []int{1, 2, 27},
			WatchedEpisodesSum: 36,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}
}
