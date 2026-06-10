package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"test/internal/domain"
	"test/internal/usecase"
)

type fakeListEditUsecase struct {
	updated    usecase.UpdatedUserAnimeListEntry
	err        error
	gotUserID  int64
	gotToken   string
	gotAnimeID int
	gotPatch   domain.UserAnimeListPatch
	calls      int
}

func (fake *fakeListEditUsecase) UpdateUserAnimeListEntry(ctx context.Context, userID int64, token string, animeID int, patch domain.UserAnimeListPatch) (usecase.UpdatedUserAnimeListEntry, error) {
	fake.calls++
	fake.gotUserID = userID
	fake.gotToken = token
	fake.gotAnimeID = animeID
	fake.gotPatch = patch
	return fake.updated, fake.err
}

type fakeAuthUsecase struct {
	token    *domain.MALToken
	tokenErr error
}

func (fake *fakeAuthUsecase) GetValidToken(ctx context.Context, userID int64) (*domain.MALToken, error) {
	return fake.token, fake.tokenErr
}

func (fake *fakeAuthUsecase) CompleteMALLogin(ctx context.Context, code, verifier string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func (fake *fakeAuthUsecase) UpsertUserByPublicUsername(ctx context.Context, username string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func (fake *fakeAuthUsecase) ResolveUserByUsername(ctx context.Context, username string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func newListEditTestAPI(listEdits ListEditUsecase, auth AuthUsecase) *HTTPAPI {
	return New(Dependencies{
		Config:    Config{SessionSecret: "test-secret"},
		Auth:      auth,
		ListEdits: listEdits,
	})
}

func sessionCookieForTest(t *testing.T, api *HTTPAPI, user domain.User) *http.Cookie {
	t.Helper()

	value, err := api.signCookiePayload(signedSessionPayload{
		UserID:    user.ID,
		Username:  user.Username,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("cannot sign session payload: %v", err)
	}

	return &http.Cookie{Name: sessionCookieName, Value: value}
}

func performListEditRequest(api *HTTPAPI, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPatch, "/api/anime/42/list-status", strings.NewReader(body))
	if cookie != nil {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	api.SetupRouter().ServeHTTP(recorder, request)
	return recorder
}

func TestUpdateListEntryHandlerSuccess(t *testing.T) {
	listEdits := &fakeListEditUsecase{
		updated: usecase.UpdatedUserAnimeListEntry{
			AnimeID:         42,
			Title:           "Test Anime",
			ListStatus:      domain.AnimeListStatusCompleted,
			Score:           9,
			WatchedEpisodes: 12,
			NumEpisodes:     12,
		},
	}
	auth := &fakeAuthUsecase{token: &domain.MALToken{AccessToken: "token-1"}}
	api := newListEditTestAPI(listEdits, auth)
	cookie := sessionCookieForTest(t, api, domain.User{ID: 7, Username: "tester"})

	recorder := performListEditRequest(api, `{"status":"completed","score":9,"num_watched_episodes":12}`, cookie)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", recorder.Code, recorder.Body.String())
	}
	if listEdits.calls != 1 || listEdits.gotUserID != 7 || listEdits.gotToken != "token-1" || listEdits.gotAnimeID != 42 {
		t.Fatalf("usecase call mismatch: %+v", listEdits)
	}
	if listEdits.gotPatch.Status == nil || *listEdits.gotPatch.Status != domain.AnimeListStatusCompleted {
		t.Fatalf("patch status mismatch: %+v", listEdits.gotPatch)
	}

	var response ListEntryResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("cannot decode response: %v", err)
	}
	if response.AnimeID != 42 || response.Status != "completed" || response.NumEpisodes != 12 {
		t.Fatalf("response mismatch: %+v", response)
	}
}

func TestUpdateListEntryHandlerRequiresSession(t *testing.T) {
	api := newListEditTestAPI(&fakeListEditUsecase{}, &fakeAuthUsecase{})

	recorder := performListEditRequest(api, `{"score":5}`, nil)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestUpdateListEntryHandlerRejectsEmptyPatch(t *testing.T) {
	listEdits := &fakeListEditUsecase{}
	auth := &fakeAuthUsecase{token: &domain.MALToken{AccessToken: "token-1"}}
	api := newListEditTestAPI(listEdits, auth)
	cookie := sessionCookieForTest(t, api, domain.User{ID: 7, Username: "tester"})

	recorder := performListEditRequest(api, `{}`, cookie)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if listEdits.calls != 0 {
		t.Fatalf("usecase must not be called for empty patch")
	}
}

func TestUpdateListEntryHandlerRejectsExpiredToken(t *testing.T) {
	listEdits := &fakeListEditUsecase{}
	auth := &fakeAuthUsecase{tokenErr: usecase.ErrTokenExpired}
	api := newListEditTestAPI(listEdits, auth)
	cookie := sessionCookieForTest(t, api, domain.User{ID: 7, Username: "tester"})

	recorder := performListEditRequest(api, `{"score":5}`, cookie)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if listEdits.calls != 0 {
		t.Fatalf("usecase must not be called without a valid token")
	}
}

func TestUpdateListEntryHandlerMapsUsecaseErrors(t *testing.T) {
	tests := []struct {
		name       string
		usecaseErr error
		wantStatus int
	}{
		{name: "unknown anime", usecaseErr: usecase.ErrAnimeNotInCatalog, wantStatus: http.StatusNotFound},
		{name: "invalid input", usecaseErr: usecase.ErrInvalidListEditInput, wantStatus: http.StatusBadRequest},
		{name: "MAL failure", usecaseErr: usecase.ErrMALListUpdateFailed, wantStatus: http.StatusBadGateway},
		{name: "save failure", usecaseErr: usecase.ErrListEntrySaveFailed, wantStatus: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			listEdits := &fakeListEditUsecase{err: test.usecaseErr}
			auth := &fakeAuthUsecase{token: &domain.MALToken{AccessToken: "token-1"}}
			api := newListEditTestAPI(listEdits, auth)
			cookie := sessionCookieForTest(t, api, domain.User{ID: 7, Username: "tester"})

			recorder := performListEditRequest(api, `{"score":5}`, cookie)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}
