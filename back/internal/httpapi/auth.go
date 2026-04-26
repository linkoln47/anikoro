package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"test/internal/usecase"
)

const malAuthorizeURL = "https://myanimelist.net/v1/oauth2/authorize"

type MeResponse struct {
	Authenticated bool         `json:"authenticated"`
	User          *UserSummary `json:"user,omitempty"`
}

type UserSummary struct {
	ID        int64  `json:"id"`
	MALUserID int64  `json:"mal_user_id,omitempty"`
	Username  string `json:"username"`
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

		authURL, err := buildMALAuthURL(api.config.ClientID, api.config.RedirectURI, state, verifier)
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

		user, err := api.auth.CompleteMALLogin(r.Context(), code, payload.Verifier)
		if err != nil {
			api.writeCompleteMALLoginError(w, err)
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

func (api *HTTPAPI) writeCompleteMALLoginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, usecase.ErrMALTokenExchangeFailed):
		api.logError("auth", "failed to exchange MAL authorization code", "err", err)
		writeAPIError(w, http.StatusBadGateway, fmt.Sprintf("Failed to exchange MAL authorization code: %v", err))
	case errors.Is(err, usecase.ErrMALCurrentUserFetchFailed):
		api.logError("auth", "failed to fetch MAL current user", "err", err)
		writeAPIError(w, http.StatusBadGateway, fmt.Sprintf("Failed to fetch MAL current user: %v", err))
	case errors.Is(err, usecase.ErrAuthUserSaveFailed):
		api.logError("auth", "failed to upsert MAL user", "err", err)
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save user: %v", err))
	case errors.Is(err, usecase.ErrAuthTokenSaveFailed):
		api.logError("auth", "failed to save MAL token", "err", err)
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save token: %v", err))
	default:
		api.logError("auth", "failed to complete MAL login", "err", err)
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to complete MAL login: %v", err))
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

func buildMALAuthURL(clientID, redirectURI, state, codeChallenge string) (string, error) {
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
