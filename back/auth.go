package main

import (
	"context"
	"crypto/rand"
	"database/sql"
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

const (
	malAuthorizeURL   = "https://myanimelist.net/v1/oauth2/authorize"
	malTokenURL       = "https://myanimelist.net/v1/oauth2/token"
	malCurrentUserURL = "https://api.myanimelist.net/v2/users/@me"
)

var (
	ErrNoValidToken = errors.New("no token stored for this user; sign in with MAL")
	ErrTokenExpired = errors.New("token expired; sign in with MAL again")
	ErrUserNotFound = errors.New("user not found")
)

type MALToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type malCurrentUserResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type MALUserProfile struct {
	ID       int64
	Username string
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
	req, err := http.NewRequest(http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
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
	req, err := http.NewRequest(http.MethodGet, malCurrentUserURL, nil)
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

	var parsed malCurrentUserResponse
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
	profile.Username = strings.TrimSpace(profile.Username)
	if profile.ID <= 0 {
		return User{}, errors.New("mal_user_id must be positive")
	}
	if profile.Username == "" {
		return User{}, errors.New("username cannot be empty")
	}

	var user User
	err := a.withTx(context.Background(), nil, func(tx *sql.Tx) error {
		err := tx.QueryRow(`
			SELECT id, mal_user_id, username
			FROM `+usersTableName+`
			WHERE mal_user_id = $1
		`, profile.ID).Scan(&user.ID, &user.MALUserID, &user.Username)
		if err == nil {
			if _, err := tx.Exec(`
				DELETE FROM `+usersTableName+`
				WHERE mal_user_id IS NULL
				  AND LOWER(username) = LOWER($1)
				  AND id <> $2
			`, profile.Username, user.ID); err != nil {
				return err
			}

			return tx.QueryRow(`
				UPDATE `+usersTableName+`
				SET username = $2,
				    updated_at = NOW()
				WHERE id = $1
				RETURNING id, mal_user_id, username
			`, user.ID, profile.Username).Scan(&user.ID, &user.MALUserID, &user.Username)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		err = tx.QueryRow(`
			UPDATE `+usersTableName+`
			SET mal_user_id = $1,
			    username = $2,
			    updated_at = NOW()
			WHERE mal_user_id IS NULL
			  AND LOWER(username) = LOWER($2)
			RETURNING id, mal_user_id, username
		`, profile.ID, profile.Username).Scan(&user.ID, &user.MALUserID, &user.Username)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		return tx.QueryRow(`
			INSERT INTO `+usersTableName+` (
				mal_user_id,
				username,
				created_at,
				updated_at
			) VALUES ($1, $2, NOW(), NOW())
			ON CONFLICT (mal_user_id) DO UPDATE
			SET username = EXCLUDED.username,
			    updated_at = NOW()
			RETURNING id, mal_user_id, username
		`, profile.ID, profile.Username).Scan(&user.ID, &user.MALUserID, &user.Username)
	})
	if err != nil {
		return User{}, err
	}

	return user, nil
}

func (a *App) upsertPublicUser(username string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("username cannot be empty")
	}

	var user User
	err := a.DB.QueryRow(`
		INSERT INTO `+usersTableName+` (
			username,
			created_at,
			updated_at
		) VALUES ($1, NOW(), NOW())
		ON CONFLICT ((LOWER(username))) DO UPDATE
		SET username = EXCLUDED.username,
		    updated_at = NOW()
		RETURNING id, username
	`, username).Scan(&user.ID, &user.Username)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

func (a *App) userByUsername(username string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("username cannot be empty")
	}

	var user User
	err := a.DB.QueryRow(`
		SELECT id, username
		FROM `+usersTableName+`
		WHERE LOWER(username) = LOWER($1)
		ORDER BY id
		LIMIT 1
	`, username).Scan(&user.ID, &user.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}

	return user, nil
}

func (a *App) loadToken(userID int64) (*MALToken, error) {
	var token MALToken

	err := a.DB.QueryRow(`
		SELECT access_token, token_type, expires_at
		FROM `+malTokensTable+`
		WHERE user_id = $1
	`, userID).Scan(&token.AccessToken, &token.TokenType, &token.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoValidToken
		}
		return nil, fmt.Errorf("load token from database: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("empty access_token in database")
	}

	return &token, nil
}

func (a *App) saveToken(userID int64, token *MALToken) error {
	if token == nil {
		return errors.New("token cannot be nil")
	}

	_, err := a.DB.Exec(`
		INSERT INTO `+malTokensTable+` (
			user_id,
			access_token,
			refresh_token,
			token_type,
			expires_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET access_token = EXCLUDED.access_token,
		    refresh_token = EXCLUDED.refresh_token,
		    token_type = EXCLUDED.token_type,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = NOW()
	`, userID, token.AccessToken, nullableString(token.RefreshToken), token.TokenType, token.ExpiresAt.UTC())
	return err
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

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
