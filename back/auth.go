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
	"os"
	"strings"
	"time"
)

const (
	malAuthorizeURL = "https://myanimelist.net/v1/oauth2/authorize"
	malTokenURL     = "https://myanimelist.net/v1/oauth2/token"
	tokenFileName   = ".mal_token.json"
)

var (
	errNoValidToken       = errors.New("no valid token available")
	errTokenRefreshFailed = errors.New("token expired and refresh failed")
)

type malToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (token *malToken) isValid(now time.Time) bool {
	return token != nil && token.AccessToken != "" && now.Before(token.ExpiresAt)
}

func (a *App) getValidToken() (*malToken, error) {
	return a.resolveStoredToken()
}

func (a *App) ensureToken() (*malToken, error) {
	if token, err := a.resolveStoredToken(); err == nil {
		return token, nil
	}

	code, verifier, err := a.authorizeWithLocalCallback()
	if err != nil {
		return nil, err
	}

	token, err := a.exchangeCodeForToken(code, verifier)
	if err != nil {
		return nil, err
	}
	if err := a.saveToken(token); err != nil {
		a.logWarn("auth", "cannot save token file", "err", err)
	}
	return token, nil
}

func (a *App) resolveStoredToken() (*malToken, error) {
	token, err := a.loadTokenFromFile()
	if err != nil {
		return nil, fmt.Errorf("load token from file: %w", err)
	}

	return a.ensureFreshToken(token)
}

func (a *App) ensureFreshToken(token *malToken) (*malToken, error) {
	if token.isValid(time.Now()) {
		return token, nil
	}

	if token.RefreshToken == "" || a.Config.ClientID == "" {
		return nil, errNoValidToken
	}

	refreshed, err := a.refreshAccessToken(token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errTokenRefreshFailed, err)
	}

	if err := a.saveToken(refreshed); err != nil {
		a.logWarn("auth", "cannot save refreshed token file", "err", err)
	}

	return refreshed, nil
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

func (a *App) exchangeCodeForToken(code, verifier string) (*malToken, error) {
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

func (a *App) refreshAccessToken(refreshToken string) (*malToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", a.Config.ClientID)
	if a.Config.ClientSecret != "" {
		form.Set("client_secret", a.Config.ClientSecret)
	}

	tok, err := a.requestTokenGrant(form, "refresh")
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

func (a *App) requestTokenGrant(form url.Values, endpointLabel string) (*malToken, error) {
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

	var tok malToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}

	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &tok, nil
}

func (a *App) loadTokenFromFile() (*malToken, error) {
	b, err := os.ReadFile(a.Config.TokenPath)
	if err != nil {
		return nil, err
	}
	var tok malToken
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, errors.New("empty access_token in token file")
	}
	return &tok, nil
}

func (a *App) saveToken(token *malToken) error {
	b, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return a.writeFileWithChangeLog(a.Config.TokenPath, b, 0o600, "Token file")
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
