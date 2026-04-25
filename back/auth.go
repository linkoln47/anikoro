package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrNoValidToken = errors.New("no token stored for this user; sign in with MAL")
	ErrTokenExpired = errors.New("token expired; sign in with MAL again")
	ErrUserNotFound = errors.New("user not found")
)

const (
	malAuthorizeURL   = "https://myanimelist.net/v1/oauth2/authorize"
	malTokenURL       = "https://myanimelist.net/v1/oauth2/token"
	malCurrentUserURL = "https://api.myanimelist.net/v2/users/@me"
)

type MALToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type User struct {
	ID        int64
	MALUserID int64
	Username  string
}

type MALUserProfile struct {
	ID       int64
	Username string
}

type currentUserResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

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
	config     *AppConfig
	httpClient *http.Client
	repo       *PostgresAuthRepository
}

func newAuthService(config *AppConfig, httpClient *http.Client, repo *PostgresAuthRepository) *AuthService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &AuthService{
		config:     config,
		httpClient: httpClient,
		repo:       repo,
	}
}

func (a *App) authService() *AuthService {
	if a.Auth != nil {
		return a.Auth
	}
	a.Auth = newAuthService(&a.Config, a.HTTPClient, a.authRepository())
	return a.Auth
}

func (token *MALToken) isValid(now time.Time) bool {
	return token != nil && token.AccessToken != "" && now.Before(token.ExpiresAt)
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
	if !token.isValid(time.Now()) {
		return nil, ErrTokenExpired
	}
	return token, nil
}

func (svc *AuthService) exchangeCodeForToken(code, verifier string) (*MALToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", svc.config.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", svc.config.RedirectURI)
	if svc.config.ClientSecret != "" {
		form.Set("client_secret", svc.config.ClientSecret)
	}

	return svc.requestTokenGrant(form, "token")
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

		authURL, err := buildAuthURL(api.config.ClientID, api.config.RedirectURI, state, verifier)
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

func (svc *AuthService) requestTokenGrant(form url.Values, endpointLabel string) (*MALToken, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := svc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s endpoint %d: %s", endpointLabel, resp.StatusCode, string(body))
	}

	var tok MALToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}

	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &tok, nil
}

func (svc *AuthService) fetchCurrentMALUser(token string) (MALUserProfile, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, malCurrentUserURL, nil)
	if err != nil {
		return MALUserProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := svc.httpClient.Do(req)
	if err != nil {
		return MALUserProfile{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return MALUserProfile{}, fmt.Errorf("current user endpoint %d: %s", resp.StatusCode, string(body))
	}

	var parsed currentUserResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return MALUserProfile{}, err
	}
	if parsed.ID <= 0 {
		return MALUserProfile{}, errors.New("current user response missing id")
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return MALUserProfile{}, errors.New("current user response missing name")
	}

	return MALUserProfile{
		ID:       parsed.ID,
		Username: strings.TrimSpace(parsed.Name),
	}, nil
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

func buildAuthURL(clientID, redirectURI, state, codeChallenge string) (string, error) {
	u, err := url.Parse(malAuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "plain")
	q.Set("redirect_uri", redirectURI)
	u.RawQuery = q.Encode()
	return u.String(), nil
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
