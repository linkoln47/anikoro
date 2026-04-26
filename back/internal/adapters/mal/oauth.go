package mal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	malTokenURL       = "https://myanimelist.net/v1/oauth2/token"
	malCurrentUserURL = "https://api.myanimelist.net/v2/users/@me"
)

type OAuthClient struct {
	httpClient *http.Client
}

var _ ports.MALOAuthClient = (*OAuthClient)(nil)

type currentUserResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func NewOAuthClient(httpClient *http.Client) *OAuthClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OAuthClient{httpClient: httpClient}
}

func (client *OAuthClient) ExchangeCodeForToken(ctx context.Context, config ports.MALOAuthConfig, code, verifier string) (*domain.MALToken, error) {
	ctx = ensureContext(ctx)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", config.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", config.RedirectURI)
	if config.ClientSecret != "" {
		form.Set("client_secret", config.ClientSecret)
	}

	return client.requestTokenGrant(ctx, form, "token")
}

func (client *OAuthClient) FetchCurrentUser(ctx context.Context, token string) (domain.MALUserProfile, error) {
	ctx = ensureContext(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, malCurrentUserURL, nil)
	if err != nil {
		return domain.MALUserProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return domain.MALUserProfile{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return domain.MALUserProfile{}, fmt.Errorf("current user endpoint %d: %s", resp.StatusCode, string(body))
	}

	var parsed currentUserResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return domain.MALUserProfile{}, err
	}
	if parsed.ID <= 0 {
		return domain.MALUserProfile{}, errors.New("current user response missing id")
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return domain.MALUserProfile{}, errors.New("current user response missing name")
	}

	return domain.MALUserProfile{
		ID:       parsed.ID,
		Username: strings.TrimSpace(parsed.Name),
	}, nil
}

func (client *OAuthClient) requestTokenGrant(ctx context.Context, form url.Values, endpointLabel string) (*domain.MALToken, error) {
	ctx = ensureContext(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, malTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s endpoint %d: %s", endpointLabel, resp.StatusCode, string(body))
	}

	var token domain.MALToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}

	token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Add(-1 * time.Minute)
	return &token, nil
}
