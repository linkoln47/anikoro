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

func TestAPI_GetAnimeHandler_ReturnsCombinedItems(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(10, "series", "Series One", 2, 9.5, "{}", 26, time.Now().UTC()).
			AddRow(3, "movie", "Movie One", 1, 8.0, "{}", 1, time.Now().UTC()),
	)
	expectCommit(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/anime/42", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

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

func TestAPI_GetStatsHandler_ReturnsCounts(t *testing.T) {
	sut, mock := newTestApp(t)

	expectStats(mock, testUserID, 2, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/stats/42", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

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

func TestAPI_SyncHandler_StartsSyncWithValidToken(t *testing.T) {
	sut, mock := newTestApp(t)

	expectLoadToken(mock, testUserID, MALToken{
		AccessToken: "valid-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	})

	startedWith := make(chan context.Context, 1)
	releaseRequest := make(chan struct{})
	requestDone := make(chan struct{})
	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			defer close(requestDone)

			if got := req.Header.Get("Authorization"); got != "Bearer valid-token" {
				t.Fatalf("authorization header = %q, want %q", got, "Bearer valid-token")
			}
			select {
			case startedWith <- req.Context():
			default:
			}
			<-releaseRequest
			return nil, fmt.Errorf("synthetic stop after observing started sync")
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

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
	case ctx := <-startedWith:
		if ctx == nil {
			t.Fatal("sync started with nil context")
		}
		select {
		case <-ctx.Done():
			t.Fatal("sync context should not be canceled when handler returns")
		default:
		}
		close(releaseRequest)
	case <-time.After(time.Second):
		t.Fatal("sync was not started")
	}

	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("background sync request did not finish")
	}
}

func TestAPI_SyncHandler_ReturnsUnauthorizedWhenNoValidTokenExists(t *testing.T) {
	sut, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM mal_tokens").
		WithArgs(testUserID).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), ErrNoValidToken.Error()) {
		t.Fatalf("response body %q does not mention %q", rec.Body.String(), ErrNoValidToken)
	}
}

func TestAPI_SyncHandler_ReturnsUnauthorizedWhenStoredTokenIsExpired(t *testing.T) {
	sut, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM mal_tokens").
		WithArgs(testUserID).
		WillReturnRows(sqlRows("access_token", "token_type", "expires_at").
			AddRow("expired-token", "Bearer", time.Now().Add(-time.Hour)))

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), ErrTokenExpired.Error()) {
		t.Fatalf("response body %q does not mention %q", rec.Body.String(), ErrTokenExpired)
	}
}

func TestAPI_SyncHandler_ReturnsInternalServerErrorWhenTokenLookupFails(t *testing.T) {
	sut, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM mal_tokens").
		WithArgs(testUserID).
		WillReturnError(fmt.Errorf("database offline"))

	req := httptest.NewRequest(http.MethodPost, "/api/sync/42", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Failed to get valid token") {
		t.Fatalf("response body %q does not mention token failure", rec.Body.String())
	}
}

func TestAPI_SyncHandler_ReturnsBadRequestWhenUserIDMissing(t *testing.T) {
	sut, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "user_id is required") {
		t.Fatalf("response body %q does not mention missing user_id", rec.Body.String())
	}
}

func TestAPI_UserIDFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/anime/15", nil)
	req = muxSetURLVars(req, map[string]string{"user_id": "15"})

	got, err := UserIDFromRequest(req)
	if err != nil {
		t.Fatalf("userIDFromRequest returned error: %v", err)
	}
	if got != 15 {
		t.Fatalf("userIDFromRequest = %d, want 15", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/anime?user_id=11", nil)
	got, err = UserIDFromRequest(req)
	if err != nil {
		t.Fatalf("userIDFromRequest with query returned error: %v", err)
	}
	if got != 11 {
		t.Fatalf("userIDFromRequest = %d, want 11", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/anime", nil)
	req.Header.Set("X-User-ID", "9")

	got, err = UserIDFromRequest(req)
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

func expectLoadToken(mock sqlmock.Sqlmock, userID int64, token MALToken) {
	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM mal_tokens").
		WithArgs(userID).
		WillReturnRows(sqlRows("access_token", "token_type", "expires_at").
			AddRow(token.AccessToken, token.TokenType, token.ExpiresAt))
}
