package tests

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	backend "test/internal/app"
)

func TestFetchCompletedAnimeEntries_FollowsPaginationAndSendsAuthHeader(t *testing.T) {
	sut, _ := newTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++

			if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
				t.Fatalf("authorization header = %q, want %q", got, "Bearer secret-token")
			}

			switch callCount {
			case 1:
				if got := req.URL.Query().Get("status"); got != "completed" {
					t.Fatalf("status query = %q, want %q", got, "completed")
				}
				if got := req.URL.Query().Get("limit"); got != "100" {
					t.Fatalf("limit query = %q, want %q", got, "100")
				}
				if got := req.URL.Query().Get("fields"); got != "list_status" {
					t.Fatalf("fields query = %q, want %q", got, "list_status")
				}
				return jsonHTTPResponse(http.StatusOK, `{
					"data": [
						{
							"node": {"id": 1, "title": "First"},
							"list_status": {"score": 9, "num_episodes_watched": 12}
						}
					],
					"paging": {"next": "https://example.test/page-2"}
				}`), nil
			case 2:
				if got := req.URL.String(); got != "https://example.test/page-2" {
					t.Fatalf("second page url = %q, want %q", got, "https://example.test/page-2")
				}
				return jsonHTTPResponse(http.StatusOK, `{
					"data": [
						{
							"node": {"id": 2, "title": "Second"},
							"list_status": {"score": 8, "num_episodes_watched": 24}
						}
					],
					"paging": {"next": ""}
				}`), nil
			default:
				t.Fatalf("unexpected request #%d", callCount)
				return nil, nil
			}
		},
	}

	entries, err := sut.FetchCompletedAnimeEntries("secret-token")
	if err != nil {
		t.Fatalf("FetchCompletedAnimeEntries returned error: %v", err)
	}

	want := []backend.AnimeEntry{
		{ID: 1, Title: "First", Score: 9, NumEpisodesWatched: 12},
		{ID: 2, Title: "Second", Score: 8, NumEpisodesWatched: 24},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries mismatch:\n got: %#v\nwant: %#v", entries, want)
	}
	if callCount != 2 {
		t.Fatalf("request count = %d, want 2", callCount)
	}
}

func TestRequestAnimeDetailsWithPlan_RetriesTransientResponsesThenSucceeds(t *testing.T) {
	sut, _ := newTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++

			if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
				t.Fatalf("authorization header = %q, want %q", got, "Bearer secret-token")
			}

			switch callCount {
			case 1:
				return textHTTPResponse(http.StatusTooManyRequests, "slow down"), nil
			case 2:
				return textHTTPResponse(http.StatusBadGateway, "upstream failed"), nil
			case 3:
				return jsonHTTPResponse(http.StatusOK, `{
					"id": 5,
					"title": "Recovered",
					"media_type": "movie",
					"related_anime": [
						{"node": {"id": 7, "title": "Linked"}},
						{"node": {"id": 0, "title": "Ignored"}}
					]
				}`), nil
			default:
				t.Fatalf("unexpected request #%d", callCount)
				return nil, nil
			}
		},
	}

	got, err := sut.RequestAnimeDetailsWithPlan("secret-token", 5, backend.AnimeDetailsRequestPlan{
		MaxAttempts:      3,
		Queue:            "retry",
		RequestTimeout:   0,
		NetworkRetryBase: 0,
		StatusRetryBase:  0,
	})
	if err != nil {
		t.Fatalf("RequestAnimeDetailsWithPlan returned error: %v", err)
	}

	want := backend.AnimeDetailsInfo{
		RelatedIDs: []int{7},
		MediaType:  "movie",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if callCount != 3 {
		t.Fatalf("request count = %d, want 3", callCount)
	}
}

func TestRequestAnimeDetailsWithPlanAndContext_StopsDuringBackoffWhenCanceled(t *testing.T) {
	sut, _ := newTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++
			return textHTTPResponse(http.StatusTooManyRequests, "slow down"), nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	startedAt := time.Now()
	_, err := sut.RequestAnimeDetailsWithPlanAndContext(ctx, "secret-token", 5, backend.AnimeDetailsRequestPlan{
		MaxAttempts:      3,
		Queue:            "retry",
		RequestTimeout:   0,
		NetworkRetryBase: 0,
		StatusRetryBase:  time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RequestAnimeDetailsWithPlanAndContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
		t.Fatalf("RequestAnimeDetailsWithPlanAndContext took too long after cancellation: %v", elapsed)
	}
	if callCount != 1 {
		t.Fatalf("request count = %d, want 1", callCount)
	}
}

func TestFetchAnimeDetailsPrimary_UsesFreshCacheWithoutHTTP(t *testing.T) {
	sut, _ := newTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++
			t.Fatalf("unexpected outbound request to %s", req.URL.String())
			return nil, nil
		},
	}

	cache := backend.NewAnimeDetailsCacheStore(sut, map[int]backend.AnimeDetailsCacheItem{
		42: {
			RelatedIDs: []int{77},
			MediaType:  "tv",
			UpdatedAt:  time.Now(),
			Resolved:   true,
		},
	}, 1000)

	got, err := sut.FetchAnimeDetailsPrimary("secret-token", 42, cache)
	if err != nil {
		t.Fatalf("FetchAnimeDetailsPrimary returned error: %v", err)
	}

	want := backend.AnimeDetailsInfo{
		RelatedIDs: []int{77},
		MediaType:  "tv",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if callCount != 0 {
		t.Fatalf("request count = %d, want 0", callCount)
	}
}

func TestFetchAnimeDetailsPrimary_UsesStaleCacheOnTransientError(t *testing.T) {
	sut, _ := newTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++
			return textHTTPResponse(http.StatusServiceUnavailable, "temporary outage"), nil
		},
	}

	cache := backend.NewAnimeDetailsCacheStore(sut, map[int]backend.AnimeDetailsCacheItem{
		42: {
			RelatedIDs: []int{99},
			MediaType:  "tv",
			UpdatedAt:  time.Now().Add(-backend.DetailsCacheTTL - time.Hour),
			Resolved:   true,
		},
	}, 1000)

	got, err := sut.FetchAnimeDetailsPrimary("secret-token", 42, cache)
	if err != nil {
		t.Fatalf("FetchAnimeDetailsPrimary returned error: %v", err)
	}

	want := backend.AnimeDetailsInfo{
		RelatedIDs: []int{99},
		MediaType:  "tv",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if callCount != 1 {
		t.Fatalf("request count = %d, want 1", callCount)
	}
}
