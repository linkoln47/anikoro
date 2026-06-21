package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"test/internal/domain"
	"test/internal/usecase"
)

// stubAuthUsecase is a configurable AuthUsecase for register/login handler
// tests. Unconfigured methods return a not-implemented error.
type stubAuthUsecase struct {
	registerUser domain.User
	registerErr  error
	loginUser    domain.User
	loginErr     error
	unlinkUser   domain.User
	unlinkErr    error
}

func (s *stubAuthUsecase) GetValidToken(ctx context.Context, userID int64) (*domain.MALToken, error) {
	return nil, errors.New("not implemented")
}

func (s *stubAuthUsecase) Register(ctx context.Context, email, username, password string) (domain.User, error) {
	return s.registerUser, s.registerErr
}

func (s *stubAuthUsecase) Authenticate(ctx context.Context, email, password string) (domain.User, error) {
	return s.loginUser, s.loginErr
}

func (s *stubAuthUsecase) CompleteMALLogin(ctx context.Context, code, verifier string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func (s *stubAuthUsecase) LinkMAL(ctx context.Context, userID int64, code, verifier string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func (s *stubAuthUsecase) UnlinkMAL(ctx context.Context, userID int64) (domain.User, error) {
	return s.unlinkUser, s.unlinkErr
}

func (s *stubAuthUsecase) UpsertUserByPublicUsername(ctx context.Context, username string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func (s *stubAuthUsecase) ResolveUserByUsername(ctx context.Context, username string) (domain.User, error) {
	return domain.User{}, errors.New("not implemented")
}

func newAuthTestAPI(auth AuthUsecase) *HTTPAPI {
	return New(Dependencies{
		Config: Config{SessionSecret: "test-secret"},
		Auth:   auth,
	})
}

func performAuthRequest(api *HTTPAPI, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	api.SetupRouter().ServeHTTP(recorder, request)
	return recorder
}

func hasSessionCookie(recorder *httptest.ResponseRecorder) bool {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return true
		}
	}
	return false
}

func TestRegisterHandlerSuccessSetsSession(t *testing.T) {
	auth := &stubAuthUsecase{registerUser: domain.User{ID: 1, Username: "Alice", Email: "alice@example.com"}}
	api := newAuthTestAPI(auth)

	recorder := performAuthRequest(api, "/api/auth/register", `{"email":"alice@example.com","username":"Alice","password":"supersecret"}`)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", recorder.Code, recorder.Body.String())
	}
	if !hasSessionCookie(recorder) {
		t.Fatal("expected a session cookie to be set")
	}

	var response MeResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Authenticated || response.User == nil || response.User.Username != "Alice" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestRegisterHandlerErrorStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"invalid email", domain.ErrInvalidEmail, http.StatusBadRequest},
		{"weak password", domain.ErrWeakPassword, http.StatusBadRequest},
		{"email taken", domain.ErrEmailTaken, http.StatusConflict},
		{"username taken", domain.ErrUsernameTaken, http.StatusConflict},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newAuthTestAPI(&stubAuthUsecase{registerErr: tc.err})
			recorder := performAuthRequest(api, "/api/auth/register", `{"email":"a@b.co","username":"Alice","password":"supersecret"}`)
			if recorder.Code != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, recorder.Code)
			}
			if hasSessionCookie(recorder) {
				t.Fatal("failed register must not set a session cookie")
			}
		})
	}
}

func TestRegisterHandlerRejectsInvalidJSON(t *testing.T) {
	api := newAuthTestAPI(&stubAuthUsecase{})
	recorder := performAuthRequest(api, "/api/auth/register", `{not json`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestLoginHandlerSuccessSetsSession(t *testing.T) {
	auth := &stubAuthUsecase{loginUser: domain.User{ID: 2, Username: "Bob", Email: "bob@example.com"}}
	api := newAuthTestAPI(auth)

	recorder := performAuthRequest(api, "/api/auth/login", `{"email":"bob@example.com","password":"supersecret"}`)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}
	if !hasSessionCookie(recorder) {
		t.Fatal("expected a session cookie to be set")
	}
}

func TestLoginHandlerInvalidCredentials(t *testing.T) {
	api := newAuthTestAPI(&stubAuthUsecase{loginErr: usecase.ErrInvalidCredentials})
	recorder := performAuthRequest(api, "/api/auth/login", `{"email":"bob@example.com","password":"wrong"}`)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if hasSessionCookie(recorder) {
		t.Fatal("failed login must not set a session cookie")
	}
}

func TestDisconnectMALHandlerRequiresSession(t *testing.T) {
	api := newAuthTestAPI(&stubAuthUsecase{})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/mal/disconnect", nil)
	recorder := httptest.NewRecorder()
	api.SetupRouter().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", recorder.Code)
	}
}

func TestDisconnectMALHandlerClearsLink(t *testing.T) {
	auth := &stubAuthUsecase{unlinkUser: domain.User{ID: 3, Username: "Carol", Email: "carol@example.com"}}
	api := newAuthTestAPI(auth)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/mal/disconnect", nil)
	request.AddCookie(sessionCookieForTest(t, api, domain.User{ID: 3, Username: "Carol", MALUserID: 42}))
	recorder := httptest.NewRecorder()
	api.SetupRouter().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}
	if !hasSessionCookie(recorder) {
		t.Fatal("expected the session cookie to be refreshed")
	}

	var response MeResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.User == nil || response.User.MALLinked {
		t.Fatalf("expected mal_linked false after disconnect, got %+v", response.User)
	}
}
