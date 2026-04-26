package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"test/internal/adapters/mal"
)

var (
	ErrNoValidToken = errors.New("no token stored for this user; sign in with MAL")
	ErrTokenExpired = errors.New("token expired; sign in with MAL again")
	ErrUserNotFound = errors.New("user not found")
)

type MeResponse struct {
	Authenticated bool         `json:"authenticated"`
	User          *UserSummary `json:"user,omitempty"`
}

type UserSummary struct {
	ID        int64  `json:"id"`
	MALUserID int64  `json:"mal_user_id,omitempty"`
	Username  string `json:"username"`
}

type AuthService struct {
	config *AppConfig
	repo   authRepository
	oauth  *mal.OAuthClient
}

type authRepository interface {
	UpsertMALUser(ctx context.Context, profile MALUserProfile) (User, error)
	UpsertPublicUser(ctx context.Context, username string) (User, error)
	UserByUsername(ctx context.Context, username string) (User, bool, error)
	LoadToken(ctx context.Context, userID int64) (MALToken, bool, error)
	SaveToken(ctx context.Context, userID int64, token MALToken) error
}

func newAuthService(config *AppConfig, httpClient *http.Client, repo authRepository, oauth *mal.OAuthClient) *AuthService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if oauth == nil {
		oauth = mal.NewOAuthClient(httpClient)
	}
	return &AuthService{
		config: config,
		repo:   repo,
		oauth:  oauth,
	}
}

func (a *App) authService() *AuthService {
	return a.Auth
}

func (svc *AuthService) getValidToken(userID int64) (*MALToken, error) {
	token, err := svc.loadToken(userID)
	if err != nil {
		return nil, err
	}

	return svc.ensureStoredTokenValid(token)
}

func (svc *AuthService) ensureStoredTokenValid(token *MALToken) (*MALToken, error) {
	if token == nil || token.AccessToken == "" {
		return nil, ErrNoValidToken
	}
	if !token.IsValid(time.Now()) {
		return nil, ErrTokenExpired
	}
	return token, nil
}

func (svc *AuthService) exchangeCodeForToken(code, verifier string) (*MALToken, error) {
	return svc.oauth.ExchangeCodeForToken(context.Background(), mal.OAuthConfig{
		ClientID:     svc.config.ClientID,
		ClientSecret: svc.config.ClientSecret,
		RedirectURI:  svc.config.RedirectURI,
	}, code, verifier)
}

func (api *HTTPAPI) startMALAuthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if api.config.ClientID == "" {
			writeAPIError(w, http.StatusInternalServerError, "MAL_CLIENT_ID is required")
			return
		}
		if api.config.RedirectURI == "" {
			writeAPIError(w, http.StatusInternalServerError, "MAL_REDIRECT_URI is required")
			return
		}

		verifier, err := randomURLSafe(64)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create OAuth verifier: %v", err))
			return
		}
		state, err := randomURLSafe(24)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create OAuth state: %v", err))
			return
		}

		cookieValue, err := api.signCookiePayload(signedOAuthPayload{
			State:     state,
			Verifier:  verifier,
			ExpiresAt: time.Now().Add(oauthMaxAge).Unix(),
		})
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to sign OAuth state: %v", err))
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     oauthCookieName,
			Value:    cookieValue,
			Path:     "/api/auth/mal",
			MaxAge:   int(oauthMaxAge.Seconds()),
			HttpOnly: true,
			Secure:   requestIsSecure(r),
			SameSite: http.SameSiteLaxMode,
		})

		authURL, err := mal.BuildAuthURL(api.config.ClientID, api.config.RedirectURI, state, verifier)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to build MAL authorization URL: %v", err))
			return
		}

		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

func (api *HTTPAPI) completeMALAuthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if malErr := strings.TrimSpace(r.URL.Query().Get("error")); malErr != "" {
			writeAPIError(w, http.StatusBadRequest, "MAL authorization failed: "+malErr)
			return
		}

		code := strings.TrimSpace(r.URL.Query().Get("code"))
		state := strings.TrimSpace(r.URL.Query().Get("state"))
		if code == "" || state == "" {
			writeAPIError(w, http.StatusBadRequest, "OAuth code and state are required")
			return
		}

		oauthCookie, err := r.Cookie(oauthCookieName)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "OAuth state cookie is missing")
			return
		}

		var payload signedOAuthPayload
		if err := api.verifyCookiePayload(oauthCookie.Value, &payload); err != nil {
			writeAPIError(w, http.StatusBadRequest, "OAuth state cookie is invalid")
			return
		}
		if time.Now().Unix() >= payload.ExpiresAt {
			writeAPIError(w, http.StatusBadRequest, "OAuth state expired")
			return
		}
		if payload.State != state || payload.Verifier == "" {
			writeAPIError(w, http.StatusBadRequest, "OAuth state mismatch")
			return
		}

		token, err := api.auth.exchangeCodeForToken(code, payload.Verifier)
		if err != nil {
			api.logError("auth", "failed to exchange MAL authorization code", "err", err)
			writeAPIError(w, http.StatusBadGateway, fmt.Sprintf("Failed to exchange MAL authorization code: %v", err))
			return
		}
		profile, err := api.auth.fetchCurrentMALUser(token.AccessToken)
		if err != nil {
			api.logError("auth", "failed to fetch MAL current user", "err", err)
			writeAPIError(w, http.StatusBadGateway, fmt.Sprintf("Failed to fetch MAL current user: %v", err))
			return
		}
		user, err := api.auth.upsertMALUser(profile)
		if err != nil {
			api.logError("auth", "failed to upsert MAL user", "username", profile.Username, "mal_user_id", profile.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save user: %v", err))
			return
		}
		if err := api.auth.saveToken(user.ID, token); err != nil {
			api.logError("auth", "failed to save MAL token", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save token: %v", err))
			return
		}
		if err := api.setSessionCookie(w, r, user); err != nil {
			api.logError("auth", "failed to set session cookie", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create session: %v", err))
			return
		}

		clearCookie(w, oauthCookieName, "/api/auth/mal")
		api.logInfo("auth", "MAL web authorization completed", "username", user.Username, "user_id", user.ID)
		http.Redirect(w, r, api.frontendRedirectURL(), http.StatusFound)
	}
}

func (api *HTTPAPI) meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeJSON(w, http.StatusOK, MeResponse{Authenticated: false})
			return
		}

		writeJSON(w, http.StatusOK, MeResponse{
			Authenticated: true,
			User: &UserSummary{
				ID:        user.ID,
				MALUserID: user.MALUserID,
				Username:  user.Username,
			},
		})
	}
}

func (api *HTTPAPI) logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearCookie(w, sessionCookieName, "/")
		writeJSON(w, http.StatusOK, SyncResponse{
			Success: true,
			Message: "Signed out",
		})
	}
}

func (api *HTTPAPI) frontendRedirectURL() string {
	redirectURL := strings.TrimSpace(api.config.FrontendURL)
	if redirectURL == "" {
		return "/"
	}
	return redirectURL
}

func (svc *AuthService) fetchCurrentMALUser(token string) (MALUserProfile, error) {
	return svc.oauth.FetchCurrentUser(context.Background(), token)
}

func (svc *AuthService) upsertMALUser(profile MALUserProfile) (User, error) {
	user, err := svc.repo.UpsertMALUser(context.Background(), profile)
	if err != nil {
		return User{}, err
	}

	return User{ID: user.ID, MALUserID: user.MALUserID, Username: user.Username}, nil
}

func (svc *AuthService) upsertPublicUser(username string) (User, error) {
	user, err := svc.repo.UpsertPublicUser(context.Background(), username)
	if err != nil {
		return User{}, err
	}

	return User{ID: user.ID, MALUserID: user.MALUserID, Username: user.Username}, nil
}

func (svc *AuthService) userByUsername(username string) (User, error) {
	user, found, err := svc.repo.UserByUsername(context.Background(), username)
	if err != nil {
		return User{}, err
	}
	if !found {
		return User{}, ErrUserNotFound
	}

	return User{ID: user.ID, MALUserID: user.MALUserID, Username: user.Username}, nil
}

func (svc *AuthService) loadToken(userID int64) (*MALToken, error) {
	token, found, err := svc.repo.LoadToken(context.Background(), userID)
	if err != nil {
		return nil, fmt.Errorf("load token from database: %w", err)
	}
	if !found {
		return nil, ErrNoValidToken
	}
	if token.AccessToken == "" {
		return nil, errors.New("empty access_token in database")
	}

	return &MALToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresIn:    token.ExpiresIn,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

func (svc *AuthService) saveToken(userID int64, token *MALToken) error {
	if token == nil {
		return errors.New("token cannot be nil")
	}

	return svc.repo.SaveToken(context.Background(), userID, MALToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresIn:    token.ExpiresIn,
		ExpiresAt:    token.ExpiresAt,
	})
}

func randomURLSafe(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("length must be > 0")
	}
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(raw)
	if len(s) > length {
		s = s[:length]
	}
	return s, nil
}
