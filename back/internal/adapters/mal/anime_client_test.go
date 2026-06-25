package mal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"test/internal/ports"
)

type animeDetailsRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip animeDetailsRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return roundTrip(req)
}

func animeDetailsTestClient(roundTrip animeDetailsRoundTripFunc) *MyAnimeListClient {
	return NewAnimeClient(&http.Client{Transport: roundTrip}, "client-id", nil)
}

func TestFetchPublicAnimeDetailsClassifiesNotFoundWithoutRetry(t *testing.T) {
	calls := 0
	client := animeDetailsTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{"message":"","error":"not_found"}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	_, err := client.FetchPublicAnimeDetails(context.Background(), 2268, ports.AnimeDetailsFetchPrimary)
	if err == nil {
		t.Fatal("FetchPublicAnimeDetails() returned nil error")
	}
	if !ports.IsAnimeDetailsNotFound(err) {
		t.Fatalf("error = %T %v, want not_found classification", err, err)
	}
	if ports.IsAnimeDetailsRetryable(err) {
		t.Fatal("404 error must not be retryable")
	}
	if calls != 1 {
		t.Fatalf("requests = %d, want 1", calls)
	}

	var fetchErr *ports.AnimeDetailsFetchError
	if !errors.As(err, &fetchErr) || fetchErr.StatusCode != http.StatusNotFound || fetchErr.AnimeID != 2268 {
		t.Fatalf("fetch error = %#v, want id=2268 status=404", fetchErr)
	}
}

func TestFetchPublicAnimeDetailsParsesGenresAndRequestsGenreField(t *testing.T) {
	var requestedURL string
	client := animeDetailsTestClient(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		body := `{
			"id": 5,
			"title": "Sample",
			"media_type": "tv",
			"genres": [
				{"id": 1, "name": "Action"},
				{"id": 0, "name": "Dropped no id"},
				{"id": 4, "name": "Comedy"}
			]
		}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})

	details, err := client.FetchPublicAnimeDetails(context.Background(), 5, ports.AnimeDetailsFetchPrimary)
	if err != nil {
		t.Fatalf("FetchPublicAnimeDetails() error = %v", err)
	}

	if !strings.Contains(requestedURL, "genres") {
		t.Fatalf("request URL %q does not request the genres field", requestedURL)
	}

	if len(details.Genres) != 2 {
		t.Fatalf("genres = %+v, want 2 entries (zero-id dropped)", details.Genres)
	}
	if details.Genres[0].ID != 1 || details.Genres[0].Name != "Action" {
		t.Fatalf("unexpected first genre: %+v", details.Genres[0])
	}
	if details.Genres[1].ID != 4 || details.Genres[1].Name != "Comedy" {
		t.Fatalf("unexpected second genre: %+v", details.Genres[1])
	}
}

func TestFetchPublicAnimeDetailsClassifiesServerErrorAsRetryable(t *testing.T) {
	client := animeDetailsTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("unavailable")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	_, err := client.FetchPublicAnimeDetails(context.Background(), 42, ports.AnimeDetailsFetchPrimary)
	if err == nil || !ports.IsAnimeDetailsRetryable(err) {
		t.Fatalf("error = %v, want retryable server error", err)
	}
}
