package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestMALClient_FetchCompletedAnimeEntries_FollowsPaginationAndSendsAuthHeader(t *testing.T) {
	sut, _ := newInternalTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = internalFakeTransport{
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
				return internalJSONHTTPResponse(http.StatusOK, `{
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
				return internalJSONHTTPResponse(http.StatusOK, `{
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

	want := []CompletedAnimeEntry{
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

func TestMALClient_FetchPublicCompletedAnimeEntries_UsesClientIDHeader(t *testing.T) {
	sut, _ := newInternalTestApp(t)
	sut.Config.ClientID = "client-id"

	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("X-MAL-CLIENT-ID"); got != "client-id" {
				t.Fatalf("client id header = %q, want %q", got, "client-id")
			}
			if got := req.Header.Get("Authorization"); got != "" {
				t.Fatalf("authorization header = %q, want empty header", got)
			}
			if got := req.URL.Path; got != "/v2/users/PublicUser/animelist" {
				t.Fatalf("request path = %q, want public user animelist path", got)
			}
			if got := req.URL.Query().Get("status"); got != "completed" {
				t.Fatalf("status query = %q, want %q", got, "completed")
			}

			return internalJSONHTTPResponse(http.StatusOK, `{
				"data": [
					{
						"node": {"id": 3, "title": "Public"},
						"list_status": {"score": 7, "num_episodes_watched": 11}
					}
				],
				"paging": {"next": ""}
			}`), nil
		},
	}

	entries, err := sut.FetchPublicCompletedAnimeEntriesWithContext(context.Background(), "PublicUser")
	if err != nil {
		t.Fatalf("FetchPublicCompletedAnimeEntriesWithContext returned error: %v", err)
	}

	want := []CompletedAnimeEntry{{ID: 3, Title: "Public", Score: 7, NumEpisodesWatched: 11}}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries mismatch:\n got: %#v\nwant: %#v", entries, want)
	}
}

func TestMALClient_RequestAnimeDetailsWithPlan_RetriesTransientResponsesThenSucceeds(t *testing.T) {
	sut, _ := newInternalTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++

			if got := req.Header.Get("Authorization"); got != "Bearer secret-token" {
				t.Fatalf("authorization header = %q, want %q", got, "Bearer secret-token")
			}

			switch callCount {
			case 1:
				return internalTextHTTPResponse(http.StatusTooManyRequests, "slow down"), nil
			case 2:
				return internalTextHTTPResponse(http.StatusBadGateway, "upstream failed"), nil
			case 3:
				return internalJSONHTTPResponse(http.StatusOK, `{
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

	got, err := sut.requestAnimeDetailsWithPlan("secret-token", 5, animeDetailsRequestPlan{
		MaxAttempts:      3,
		Queue:            "retry",
		RequestTimeout:   0,
		NetworkRetryBase: 0,
		StatusRetryBase:  0,
	})
	if err != nil {
		t.Fatalf("requestAnimeDetailsWithPlan returned error: %v", err)
	}

	want := AnimeDetails{
		ID:        5,
		Title:     "Recovered",
		MediaType: "movie",
		Related: []AnimeRelation{
			{ID: 7, Title: "Linked"},
		},
		RelatedIDs: []int{7},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if callCount != 3 {
		t.Fatalf("request count = %d, want 3", callCount)
	}
}

func TestMALClient_RequestAnimeDetailsWithPlan_UsesClientIDAuth(t *testing.T) {
	sut, _ := newInternalTestApp(t)

	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("X-MAL-CLIENT-ID"); got != "client-id" {
				t.Fatalf("client id header = %q, want %q", got, "client-id")
			}
			if got := req.Header.Get("Authorization"); got != "" {
				t.Fatalf("authorization header = %q, want empty header", got)
			}

			return internalJSONHTTPResponse(http.StatusOK, `{
				"id": 5,
				"title": "Public Details",
				"media_type": "tv",
				"related_anime": []
			}`), nil
		},
	}

	got, err := sut.requestAnimeDetailsWithPlanAndAuthContext(context.Background(), clientIDMALAuth("client-id"), 5, animeDetailsRequestPlan{
		MaxAttempts:      1,
		Queue:            "primary",
		RequestTimeout:   0,
		NetworkRetryBase: 0,
		StatusRetryBase:  0,
	})
	if err != nil {
		t.Fatalf("requestAnimeDetailsWithPlanAndAuthContext returned error: %v", err)
	}

	want := AnimeDetails{
		ID:         5,
		Title:      "Public Details",
		MediaType:  "tv",
		Related:    []AnimeRelation{},
		RelatedIDs: []int{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("details mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestMALClient_RequestAnimeDetailsWithPlanAndContext_StopsDuringBackoffWhenCanceled(t *testing.T) {
	sut, _ := newInternalTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++
			return internalTextHTTPResponse(http.StatusTooManyRequests, "slow down"), nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	startedAt := time.Now()
	_, err := sut.requestAnimeDetailsWithPlanAndContext(ctx, "secret-token", 5, animeDetailsRequestPlan{
		MaxAttempts:      3,
		Queue:            "retry",
		RequestTimeout:   0,
		NetworkRetryBase: 0,
		StatusRetryBase:  time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("requestAnimeDetailsWithPlanAndContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(startedAt); elapsed >= 500*time.Millisecond {
		t.Fatalf("requestAnimeDetailsWithPlanAndContext took too long after cancellation: %v", elapsed)
	}
	if callCount != 1 {
		t.Fatalf("request count = %d, want 1", callCount)
	}
}

func TestMALClient_FetchAnimeDetailsPrimary_UsesFreshCacheWithoutHTTP(t *testing.T) {
	sut, _ := newInternalTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++
			t.Fatalf("unexpected outbound request to %s", req.URL.String())
			return nil, nil
		},
	}

	cache := newAnimeDetailsCacheStore(sut, map[int]animeDetailsCacheItem{
		42: {
			RelatedIDs: []int{77},
			MediaType:  "tv",
			UpdatedAt:  time.Now(),
			Resolved:   true,
		},
	}, 1000)

	got, err := sut.fetchAnimeDetailsPrimary("secret-token", 42, cache)
	if err != nil {
		t.Fatalf("fetchAnimeDetailsPrimary returned error: %v", err)
	}

	want := AnimeDetails{
		ID:         42,
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

func TestMALClient_FetchAnimeDetailsPrimary_UsesStaleCacheOnTransientError(t *testing.T) {
	sut, _ := newInternalTestApp(t)

	callCount := 0
	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			callCount++
			return internalTextHTTPResponse(http.StatusServiceUnavailable, "temporary outage"), nil
		},
	}

	cache := newAnimeDetailsCacheStore(sut, map[int]animeDetailsCacheItem{
		42: {
			RelatedIDs: []int{99},
			MediaType:  "tv",
			UpdatedAt:  time.Now().Add(-DetailsCacheTTL - time.Hour),
			Resolved:   true,
		},
	}, 1000)

	got, err := sut.fetchAnimeDetailsPrimary("secret-token", 42, cache)
	if err != nil {
		t.Fatalf("fetchAnimeDetailsPrimary returned error: %v", err)
	}

	want := AnimeDetails{
		ID:         42,
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
