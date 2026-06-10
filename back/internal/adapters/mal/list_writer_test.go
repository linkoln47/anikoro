package mal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"test/internal/domain"
)

// rewriteHostTransport redirects every request to the test server while
// preserving the request path, so production MAL URLs stay untouched.
type rewriteHostTransport struct {
	targetURL *url.URL
}

func (transport rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = transport.targetURL.Scheme
	req.URL.Host = transport.targetURL.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newTestClientForServer(t *testing.T, server *httptest.Server) *MyAnimeListClient {
	t.Helper()

	targetURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("cannot parse test server URL: %v", err)
	}

	httpClient := &http.Client{Transport: rewriteHostTransport{targetURL: targetURL}}
	return NewAnimeClient(httpClient, "client-id", nil)
}

func TestUpdateAnimeListStatusSendsPatchAndParsesResponse(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAuth   string
		gotForm   url.Values
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("cannot read request body: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Errorf("cannot parse form body: %v", err)
		}
		gotForm = form

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"watching","score":7,"num_episodes_watched":5}`))
	}))
	defer server.Close()

	client := newTestClientForServer(t, server)

	status := domain.AnimeListStatusWatching
	score := 7
	episodes := 5
	state, err := client.UpdateAnimeListStatus(context.Background(), "token-1", 42, domain.UserAnimeListPatch{
		Status:          &status,
		Score:           &score,
		WatchedEpisodes: &episodes,
	})
	if err != nil {
		t.Fatalf("UpdateAnimeListStatus() returned error: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/v2/anime/42/my_list_status" {
		t.Fatalf("path = %q, want /v2/anime/42/my_list_status", gotPath)
	}
	if gotAuth != "Bearer token-1" {
		t.Fatalf("authorization = %q, want bearer token", gotAuth)
	}
	if gotForm.Get("status") != "watching" || gotForm.Get("score") != "7" || gotForm.Get("num_watched_episodes") != "5" {
		t.Fatalf("form = %v, want status/score/num_watched_episodes", gotForm)
	}
	if state.ListStatus != "watching" || state.Score != 7 || state.WatchedEpisodes != 5 {
		t.Fatalf("state = %+v, want canonical MAL values", state)
	}
}

func TestUpdateAnimeListStatusOmitsUnsetFields(t *testing.T) {
	var gotForm url.Values

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"watching","score":0,"num_episodes_watched":6}`))
	}))
	defer server.Close()

	client := newTestClientForServer(t, server)

	episodes := 6
	if _, err := client.UpdateAnimeListStatus(context.Background(), "token", 42, domain.UserAnimeListPatch{WatchedEpisodes: &episodes}); err != nil {
		t.Fatalf("UpdateAnimeListStatus() returned error: %v", err)
	}

	if _, hasStatus := gotForm["status"]; hasStatus {
		t.Fatalf("status must not be sent when unset, form = %v", gotForm)
	}
	if _, hasScore := gotForm["score"]; hasScore {
		t.Fatalf("score must not be sent when unset, form = %v", gotForm)
	}
	if gotForm.Get("num_watched_episodes") != "6" {
		t.Fatalf("num_watched_episodes = %q, want 6", gotForm.Get("num_watched_episodes"))
	}
}

func TestUpdateAnimeListStatusRejectsEmptyPatch(t *testing.T) {
	client := NewAnimeClient(http.DefaultClient, "client-id", nil)
	if _, err := client.UpdateAnimeListStatus(context.Background(), "token", 42, domain.UserAnimeListPatch{}); err == nil {
		t.Fatal("expected an error for empty patch")
	}
}

func TestUpdateAnimeListStatusReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClientForServer(t, server)

	score := 7
	if _, err := client.UpdateAnimeListStatus(context.Background(), "token", 42, domain.UserAnimeListPatch{Score: &score}); err == nil {
		t.Fatal("expected an error for non-200 MAL response")
	}
}
