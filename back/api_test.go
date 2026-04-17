package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGetAnimeHandler_ReturnsCombinedItems(t *testing.T) {
	app := newTestApp(t)

	err := app.saveGroupedLists(
		[]groupedView{
			{
				ID:                 10,
				GroupKey:           "10:11",
				DisplayTitle:       "Series One",
				MergedTitles:       2,
				AvgScore:           9.5,
				WatchedEpisodesSum: 26,
			},
		},
		[]groupedView{
			{
				ID:                 3,
				GroupKey:           "3",
				DisplayTitle:       "Movie One",
				MergedTitles:       1,
				AvgScore:           8,
				WatchedEpisodesSum: 1,
			},
		},
	)
	if err != nil {
		t.Fatalf("saveGroupedLists: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/anime", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var items []AnimeItem
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode anime response: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("anime response item count = %d, want 2", len(items))
	}
	if items[0].ID != 10 || items[0].Type != "series" || items[0].DisplayTitle != "Series One" {
		t.Fatalf("first anime item = %#v, want series row", items[0])
	}
	if items[1].ID != 3 || items[1].Type != "movie" || items[1].DisplayTitle != "Movie One" {
		t.Fatalf("second anime item = %#v, want movie row", items[1])
	}
	for _, item := range items {
		if _, err := time.Parse(time.RFC3339, item.SyncedAt); err != nil {
			t.Fatalf("synced_at %q is not RFC3339: %v", item.SyncedAt, err)
		}
	}
}

func TestGetStatsHandler_ReturnsCounts(t *testing.T) {
	app := newTestApp(t)

	err := app.saveGroupedLists(
		[]groupedView{
			{ID: 10, GroupKey: "10", DisplayTitle: "Series One", MergedTitles: 1, AvgScore: 7, WatchedEpisodesSum: 12},
			{ID: 20, GroupKey: "20", DisplayTitle: "Series Two", MergedTitles: 1, AvgScore: 8, WatchedEpisodesSum: 24},
		},
		[]groupedView{
			{ID: 3, GroupKey: "3", DisplayTitle: "Movie One", MergedTitles: 1, AvgScore: 9, WatchedEpisodesSum: 1},
		},
	)
	if err != nil {
		t.Fatalf("saveGroupedLists: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var stats StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats response: %v", err)
	}

	want := StatsResponse{SeriesCount: 2, MoviesCount: 1, TotalCount: 3}
	if stats != want {
		t.Fatalf("stats = %#v, want %#v", stats, want)
	}
}

func TestSyncHandler_StartsSyncWithValidToken(t *testing.T) {
	app := newTestApp(t)
	writeTestToken(t, app.Config.TokenPath, malToken{
		AccessToken: "valid-token",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	startedWith := make(chan string, 1)
	app.StartSync = func(token string) {
		startedWith <- token
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response SyncResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode sync response: %v", err)
	}
	if !response.Success || response.Message != "Sync started in background" {
		t.Fatalf("sync response = %#v, want success response", response)
	}

	select {
	case got := <-startedWith:
		if got != "valid-token" {
			t.Fatalf("sync started with token %q, want %q", got, "valid-token")
		}
	case <-time.After(time.Second):
		t.Fatal("sync was not started")
	}
}

func TestSyncHandler_ReturnsUnauthorizedWhenNoValidTokenExists(t *testing.T) {
	app := newTestApp(t)
	writeTestToken(t, app.Config.TokenPath, malToken{
		AccessToken: "expired-token",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), errNoValidToken.Error()) {
		t.Fatalf("response body %q does not mention %q", rec.Body.String(), errNoValidToken)
	}
}

func TestSyncHandler_ReturnsInternalServerErrorForBrokenTokenFile(t *testing.T) {
	app := newTestApp(t)

	if err := os.WriteFile(app.Config.TokenPath, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("write broken token file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Failed to get valid token") {
		t.Fatalf("response body %q does not mention token failure", rec.Body.String())
	}
}
