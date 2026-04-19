package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gorilla/mux"
)

func TestGetAnimeHandler_ReturnsCombinedItems(t *testing.T) {
	app, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "watched_episodes_sum", "synced_at").
			AddRow(10, "series", "Series One", 2, 9.5, 26, time.Now().UTC()).
			AddRow(3, "movie", "Movie One", 1, 8.0, 1, time.Now().UTC()),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/anime/42", nil)
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
	app, mock := newTestApp(t)

	expectStats(mock, testUserID, 2, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/stats/42", nil)
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
	app, mock := newTestApp(t)

	expectLoadToken(mock, testUserID, malToken{
		AccessToken: "valid-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	type syncCall struct {
		Ctx    context.Context
		UserID int64
		Token  string
	}
	startedWith := make(chan syncCall, 1)
	app.StartSync = func(ctx context.Context, userID int64, token string) {
		startedWith <- syncCall{Ctx: ctx, UserID: userID, Token: token}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
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
		if got.Ctx == nil {
			t.Fatal("sync started with nil context")
		}
		select {
		case <-got.Ctx.Done():
			t.Fatal("sync context should not be canceled when handler returns")
		default:
		}
		if got.UserID != testUserID {
			t.Fatalf("sync started with user_id %d, want %d", got.UserID, testUserID)
		}
		if got.Token != "valid-token" {
			t.Fatalf("sync started with token %q, want %q", got.Token, "valid-token")
		}
	case <-time.After(time.Second):
		t.Fatal("sync was not started")
	}
}

func TestSyncHandler_ReturnsUnauthorizedWhenNoValidTokenExists(t *testing.T) {
	app, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM " + malTokensTable).
		WithArgs(testUserID).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), errNoValidToken.Error()) {
		t.Fatalf("response body %q does not mention %q", rec.Body.String(), errNoValidToken)
	}
}

func TestSyncHandler_ReturnsUnauthorizedWhenStoredTokenIsExpired(t *testing.T) {
	app, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM " + malTokensTable).
		WithArgs(testUserID).
		WillReturnRows(sqlRows("access_token", "token_type", "expires_at").
			AddRow("expired-token", "Bearer", time.Now().Add(-time.Hour)))

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), errTokenExpired.Error()) {
		t.Fatalf("response body %q does not mention %q", rec.Body.String(), errTokenExpired)
	}
}

func TestSyncHandler_ReturnsInternalServerErrorWhenTokenLookupFails(t *testing.T) {
	app, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM " + malTokensTable).
		WithArgs(testUserID).
		WillReturnError(fmt.Errorf("database offline"))

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Failed to get valid token") {
		t.Fatalf("response body %q does not mention token failure", rec.Body.String())
	}
}

func TestSyncHandler_ReturnsBadRequestWhenUserIDMissing(t *testing.T) {
	app, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()

	app.setupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "user_id is required") {
		t.Fatalf("response body %q does not mention missing user_id", rec.Body.String())
	}
}

func TestUserIDFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/anime/15", nil)
	req = muxSetURLVars(req, map[string]string{"user_id": "15"})

	got, err := userIDFromRequest(req)
	if err != nil {
		t.Fatalf("userIDFromRequest returned error: %v", err)
	}
	if got != 15 {
		t.Fatalf("userIDFromRequest = %d, want 15", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/anime?user_id=11", nil)
	got, err = userIDFromRequest(req)
	if err != nil {
		t.Fatalf("userIDFromRequest with query returned error: %v", err)
	}
	if got != 11 {
		t.Fatalf("userIDFromRequest = %d, want 11", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/anime", nil)
	req.Header.Set("X-User-ID", "9")

	got, err = userIDFromRequest(req)
	if err != nil {
		t.Fatalf("userIDFromRequest with header returned error: %v", err)
	}
	if got != 9 {
		t.Fatalf("userIDFromRequest = %d, want 9", got)
	}
}

func muxSetURLVars(req *http.Request, vars map[string]string) *http.Request {
	return mux.SetURLVars(req, vars)
}

func expectLoadToken(mock sqlmock.Sqlmock, userID int64, token malToken) {
	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM " + malTokensTable).
		WithArgs(userID).
		WillReturnRows(sqlRows("access_token", "token_type", "expires_at").
			AddRow(token.AccessToken, token.TokenType, token.ExpiresAt))
}
