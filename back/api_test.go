package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAPI_GetAnimeHandler_ReturnsCombinedItemsForSessionUser(t *testing.T) {
	sut, mock := newTestApp(t)

	expectAnimeList(mock, testUserID,
		sqlRows("anime_id", "anime_type", "display_title", "merged_titles", "avg_score", "group_member_ids", "watched_episodes_sum", "synced_at").
			AddRow(10, "series", "Series One", 2, 9.5, "{}", 26, time.Now().UTC()).
			AddRow(3, "movie", "Movie One", 1, 8.0, "{}", 1, time.Now().UTC()),
	)
	expectCommit(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/anime", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
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

func TestAPI_GetAnimeHandler_ReturnsUnauthorizedWithoutSession(t *testing.T) {
	sut, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/anime", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAPI_GetStatsHandler_ReturnsCountsForSessionUser(t *testing.T) {
	sut, mock := newTestApp(t)

	expectStats(mock, testUserID, 2, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
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

func TestAPI_MeHandler_ReturnsSessionUser(t *testing.T) {
	sut, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response MeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if !response.Authenticated || response.User == nil || response.User.ID != testUserID || response.User.Username != "test-user" {
		t.Fatalf("me response = %#v, want authenticated test user", response)
	}
}

func TestAPI_MeHandler_ReturnsAnonymousStateWithoutSession(t *testing.T) {
	sut, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	var response MeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	if response.Authenticated || response.User != nil {
		t.Fatalf("me response = %#v, want anonymous state", response)
	}
}

func TestAPI_LogoutHandler_ClearsSessionCookie(t *testing.T) {
	sut, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}

	cleared := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout response did not clear session cookie")
	}
}

func TestAPI_SyncHandler_StartsSyncWithValidSessionAndToken(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
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

func TestAPI_SyncHandler_ReturnsUnauthorizedWithoutSession(t *testing.T) {
	sut, _ := newTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAPI_SyncHandler_ReturnsUnauthorizedWhenNoValidTokenExists(t *testing.T) {
	sut, mock := newTestApp(t)

	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM mal_tokens").
		WithArgs(testUserID).
		WillReturnError(sql.ErrNoRows)

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
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

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
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

	req := httptest.NewRequest(http.MethodPost, "/api/sync", nil)
	addSessionCookie(t, sut, req, testUserID, "test-user")
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Failed to get valid token") {
		t.Fatalf("response body %q does not mention token failure", rec.Body.String())
	}
}

func TestAPI_StartMALAuthHandler_RedirectsToMALAndSetsStateCookie(t *testing.T) {
	sut, _ := newTestApp(t)
	sut.Config.ClientID = "client-id"
	sut.Config.RedirectURI = "http://localhost:8080/api/auth/mal/callback"

	req := httptest.NewRequest(http.MethodGet, "/api/auth/mal/start", nil)
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusFound)
	}

	location := rec.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if parsed.Host != "myanimelist.net" || parsed.Path != "/v1/oauth2/authorize" {
		t.Fatalf("redirect location = %q, want MAL authorize URL", location)
	}
	if parsed.Query().Get("client_id") != "client-id" || parsed.Query().Get("redirect_uri") != sut.Config.RedirectURI {
		t.Fatalf("redirect query = %s, missing client id or redirect uri", parsed.RawQuery)
	}

	foundStateCookie := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == oauthCookieName && cookie.Value != "" && cookie.HttpOnly {
			foundStateCookie = true
		}
	}
	if !foundStateCookie {
		t.Fatal("auth start response did not set OAuth state cookie")
	}
}

func TestAPI_CompleteMALAuthHandler_SavesTokenAndSetsSession(t *testing.T) {
	sut, mock := newTestApp(t)
	sut.Config.ClientID = "client-id"
	sut.Config.ClientSecret = "client-secret"
	sut.Config.RedirectURI = "http://localhost:8080/api/auth/mal/callback"
	sut.Config.FrontendURL = "http://localhost:5173/"

	oauthCookieValue, err := sut.signCookiePayload(signedOAuthPayload{
		State:     "state-value",
		Verifier:  "verifier-value",
		ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("sign oauth cookie: %v", err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO users")).
		WithArgs("test-user").
		WillReturnRows(sqlRows("id", "username").AddRow(testUserID, "test-user"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO mal_tokens")).
		WithArgs(testUserID, "access-token", "refresh-token", "Bearer", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	sut.HTTPClient.Transport = fakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case malTokenURL:
				if got := req.FormValue("code"); got != "code-value" {
					t.Fatalf("token form code = %q, want code-value", got)
				}
				if got := req.FormValue("code_verifier"); got != "verifier-value" {
					t.Fatalf("token form verifier = %q, want verifier-value", got)
				}
				return jsonHTTPResponse(http.StatusOK, `{
					"access_token": "access-token",
					"refresh_token": "refresh-token",
					"token_type": "Bearer",
					"expires_in": 3600
				}`), nil
			case malCurrentUserURL:
				if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
					t.Fatalf("current user authorization = %q, want bearer token", got)
				}
				return jsonHTTPResponse(http.StatusOK, `{"name": "test-user"}`), nil
			default:
				return nil, fmt.Errorf("unexpected outbound request: %s %s", req.Method, req.URL.String())
			}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/auth/mal/callback?state=state-value&code=code-value", nil)
	req.AddCookie(&http.Cookie{Name: oauthCookieName, Value: oauthCookieValue})
	rec := httptest.NewRecorder()

	sut.SetupRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "http://localhost:5173/" {
		t.Fatalf("callback redirect = %q, want frontend URL", got)
	}

	foundSessionCookie := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" && cookie.HttpOnly {
			foundSessionCookie = true
		}
	}
	if !foundSessionCookie {
		t.Fatal("callback response did not set session cookie")
	}
}

func addSessionCookie(t *testing.T, app *App, req *http.Request, userID int64, username string) {
	t.Helper()

	value, err := app.signCookiePayload(signedSessionPayload{
		UserID:    userID,
		Username:  username,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign session cookie: %v", err)
	}

	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
}

func expectLoadToken(mock sqlmock.Sqlmock, userID int64, token MALToken) {
	mock.ExpectQuery("SELECT access_token, token_type, expires_at\\s+FROM mal_tokens").
		WithArgs(userID).
		WillReturnRows(sqlRows("access_token", "token_type", "expires_at").
			AddRow(token.AccessToken, token.TokenType, token.ExpiresAt))
}
