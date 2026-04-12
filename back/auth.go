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
	tokenFilePath = appFilePath(tokenFileName)
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

func getValidToken(clientID, clientSecret string) (*malToken, error) {
	token, err := loadTokenFromFile()
	if err != nil {
		return nil, fmt.Errorf("load token from file: %w", err)
	}

	if token.AccessToken != "" && time.Now().Before(token.ExpiresAt) {
		return token, nil
	}

	if token.RefreshToken == "" || clientID == "" || clientSecret == "" {
		return nil, errNoValidToken
	}

	refreshed, err := refreshAccessToken(clientID, clientSecret, token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errTokenRefreshFailed, err)
	}

	if err := saveToken(refreshed); err != nil {
		logWarn("auth", "cannot save refreshed token file", "err", err)
	}

	return refreshed, nil
}

func ensureToken(clientID, clientSecret, redirectURI string) (*malToken, error) {
	if token, err := loadTokenFromFile(); err == nil {
		if token.AccessToken != "" && time.Now().Before(token.ExpiresAt) {
			return token, nil
		}
		if token.RefreshToken != "" {
			refreshed, refreshErr := refreshAccessToken(clientID, clientSecret, token.RefreshToken)
			if refreshErr == nil {
				_ = saveToken(refreshed)
				return refreshed, nil
			}
		}
	}

	code, verifier, err := authorizeWithLocalCallback(clientID, redirectURI)
	if err != nil {
		return nil, err
	}

	token, err := exchangeCodeForToken(clientID, clientSecret, redirectURI, code, verifier)
	if err != nil {
		return nil, err
	}
	if err := saveToken(token); err != nil {
		logWarn("auth", "cannot save token file", "err", err)
	}
	return token, nil
}

func authorizeWithLocalCallback(clientID, redirectURI string) (code string, verifier string, err error) {
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

func exchangeCodeForToken(clientID, clientSecret, redirectURI, code, verifier string) (*malToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint %d: %s", resp.StatusCode, string(body))
	}

	var tok malToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &tok, nil
}

func refreshAccessToken(clientID, clientSecret, refreshToken string) (*malToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequest(http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh endpoint %d: %s", resp.StatusCode, string(body))
	}

	var tok malToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	tok.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &tok, nil
}

func loadTokenFromFile() (*malToken, error) {
	b, err := os.ReadFile(tokenFilePath)
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

func saveToken(token *malToken) error {
	b, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFilePath, b, 0o600)
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
