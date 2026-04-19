package app

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
	ErrNoValidToken = errors.New("no token stored for this user; run `go run ./cmd/api auth`")
	ErrTokenExpired = errors.New("token expired; run `go run ./cmd/api auth` again")
)

type MALToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type malCurrentUserResponse struct {
	Name string `json:"name"`
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

func (a *App) authorizeUserToken() (*MALToken, error) {
	code, verifier, err := a.authorizeWithLocalCallback()
	if err != nil {
		return nil, err
	}

	return a.exchangeCodeForToken(code, verifier)
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

func (a *App) authorizeWithLocalCallback() (code string, verifier string, err error) {
	clientID := a.Config.ClientID
	redirectURI := a.Config.RedirectURI

	verifier, err = randomURLSafe(64)
	if err != nil {
		return "", "", err
	}
	state, err := randomURLSafe(24)
	if err != nil {
		return "", "", err
	}

	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		return "", "", fmt.Errorf("invalid MAL_REDIRECT_URL: %w", err)
	}
	if redirectURL.Host == "" {
		return "", "", errors.New("MAL_REDIRECT_URL must include host")
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{Addr: redirectURL.Host}
	mux := http.NewServeMux()
	srv.Handler = mux

	mux.HandleFunc(redirectURL.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			select {
			case errCh <- errors.New("state mismatch"):
			default:
			}
			return
		}
		codeVal := q.Get("code")
		if codeVal == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- errors.New("missing code in callback"):
			default:
			}
			return
		}
		_, _ = w.Write([]byte("Authorization completed. You can return to terminal."))
		select {
		case codeCh <- codeVal:
		default:
		}
	})

	go func() {
		if listenErr := srv.ListenAndServe(); listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			select {
			case errCh <- listenErr:
			default:
			}
		}
	}()

	authURL, err := buildAuthURL(clientID, redirectURI, state, verifier)
	if err != nil {
		_ = srv.Shutdown(context.Background())
		return "", "", err
	}
	a.logInfo("auth", "open MAL authorization URL", "url", authURL)
	a.logInfo("auth", "waiting for MAL callback", "redirect_uri", redirectURI)

	select {
	case code = <-codeCh:
		_ = srv.Shutdown(context.Background())
		return code, verifier, nil
	case listenErr := <-errCh:
		_ = srv.Shutdown(context.Background())
		return "", "", listenErr
	case <-time.After(3 * time.Minute):
		_ = srv.Shutdown(context.Background())
		return "", "", errors.New("authorization timeout")
	}
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

func (a *App) fetchCurrentUsername(token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, malCurrentUserURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("current user endpoint %d: %s", resp.StatusCode, string(body))
	}

	var parsed malCurrentUserResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return "", errors.New("current user response missing name")
	}

	return strings.TrimSpace(parsed.Name), nil
}

func (a *App) upsertUser(username string) (User, error) {
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
		ON CONFLICT (username) DO UPDATE
		SET username = EXCLUDED.username,
		    updated_at = NOW()
		RETURNING id, username
	`, username).Scan(&user.ID, &user.Username)
	if err != nil {
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
