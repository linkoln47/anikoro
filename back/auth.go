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

func (token *MALToken) isValid(now time.Time) bool {
	return token != nil && token.AccessToken != "" && now.Before(token.ExpiresAt)
}

func (a *App) getValidToken(userID int64) (*MALToken, error) {
	token, err := a.loadToken(userID)
	if err != nil {
		return nil, err
	}

	return a.ensureStoredTokenValid(token)
}

func (a *App) ensureStoredTokenValid(token *MALToken) (*MALToken, error) {
	if token == nil || token.AccessToken == "" {
		return nil, ErrNoValidToken
	}
	if !token.isValid(time.Now()) {
		return nil, ErrTokenExpired
	}
	return token, nil
}

func (a *App) exchangeCodeForToken(code, verifier string) (*MALToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", a.Config.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", a.Config.RedirectURI)
	if a.Config.ClientSecret != "" {
		form.Set("client_secret", a.Config.ClientSecret)
	}

	return a.requestTokenGrant(form, "token")
}

func (a *App) startMALAuthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.Config.ClientID == "" {
			writeAPIError(w, http.StatusInternalServerError, "MAL_CLIENT_ID is required")
			return
		}
		if a.Config.RedirectURI == "" {
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

		cookieValue, err := a.signCookiePayload(signedOAuthPayload{
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

		authURL, err := buildAuthURL(a.Config.ClientID, a.Config.RedirectURI, state, verifier)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to build MAL authorization URL: %v", err))
			return
		}

		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

func (a *App) completeMALAuthHandler() http.HandlerFunc {
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
		if err := a.verifyCookiePayload(oauthCookie.Value, &payload); err != nil {
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

		token, err := a.exchangeCodeForToken(code, payload.Verifier)
		if err != nil {
			a.logError("auth", "failed to exchange MAL authorization code", "err", err)
			writeAPIError(w, http.StatusBadGateway, fmt.Sprintf("Failed to exchange MAL authorization code: %v", err))
			return
		}
		profile, err := a.fetchCurrentMALUser(token.AccessToken)
		if err != nil {
			a.logError("auth", "failed to fetch MAL current user", "err", err)
			writeAPIError(w, http.StatusBadGateway, fmt.Sprintf("Failed to fetch MAL current user: %v", err))
			return
		}
		user, err := a.upsertMALUser(profile)
		if err != nil {
			a.logError("auth", "failed to upsert MAL user", "username", profile.Username, "mal_user_id", profile.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save user: %v", err))
			return
		}
		if err := a.saveToken(user.ID, token); err != nil {
			a.logError("auth", "failed to save MAL token", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save token: %v", err))
			return
		}
		if err := a.setSessionCookie(w, r, user); err != nil {
			a.logError("auth", "failed to set session cookie", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create session: %v", err))
			return
		}

		clearCookie(w, oauthCookieName, "/api/auth/mal")
		a.logInfo("auth", "MAL web authorization completed", "username", user.Username, "user_id", user.ID)
		http.Redirect(w, r, a.frontendRedirectURL(), http.StatusFound)
	}
}

func (a *App) meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.currentUserFromRequest(r)
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

func (a *App) logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clearCookie(w, sessionCookieName, "/")
		writeJSON(w, http.StatusOK, SyncResponse{
			Success: true,
			Message: "Signed out",
		})
	}
}

func (a *App) frontendRedirectURL() string {
	redirectURL := strings.TrimSpace(a.Config.FrontendURL)
	if redirectURL == "" {
		return "/"
	}
	return redirectURL
}

func (a *App) requestTokenGrant(form url.Values, endpointLabel string) (*MALToken, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.HTTPClient.Do(req)
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

func (a *App) fetchCurrentMALUser(token string) (MALUserProfile, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, malCurrentUserURL, nil)
	if err != nil {
		return MALUserProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
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

func (a *App) upsertMALUser(profile MALUserProfile) (User, error) {
	user, err := a.authRepository().UpsertMALUser(context.Background(), profile)
	if err != nil {
		return User{}, err
	}

	return User{ID: user.ID, MALUserID: user.MALUserID, Username: user.Username}, nil
}

func (a *App) upsertPublicUser(username string) (User, error) {
	user, err := a.authRepository().UpsertPublicUser(context.Background(), username)
	if err != nil {
		return User{}, err
	}

	return User{ID: user.ID, MALUserID: user.MALUserID, Username: user.Username}, nil
}

func (a *App) userByUsername(username string) (User, error) {
	user, found, err := a.authRepository().UserByUsername(context.Background(), username)
	if err != nil {
		return User{}, err
	}
	if !found {
		return User{}, ErrUserNotFound
	}

	return User{ID: user.ID, MALUserID: user.MALUserID, Username: user.Username}, nil
}

func (a *App) loadToken(userID int64) (*MALToken, error) {
	token, found, err := a.authRepository().LoadToken(context.Background(), userID)
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

func (a *App) saveToken(userID int64, token *MALToken) error {
	if token == nil {
		return errors.New("token cannot be nil")
	}

	return a.authRepository().SaveToken(context.Background(), userID, MALToken{
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
