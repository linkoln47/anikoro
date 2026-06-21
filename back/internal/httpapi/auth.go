package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"test/internal/domain"
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
	Email     string `json:"email,omitempty"`
	MALLinked bool   `json:"mal_linked"`
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func toUserSummary(user domain.User) *UserSummary {
	return &UserSummary{
		ID:        user.ID,
		MALUserID: user.MALUserID,
		Username:  user.Username,
		Email:     user.Email,
		MALLinked: user.MALLinked(),
	}
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

		// If a native account is already signed in, link MAL to it instead of
		// creating a standalone MAL session. The anime snapshot stays keyed by
		// the same user_id, so sync keeps working unchanged.
		if existing, sessionErr := api.currentUserFromRequest(r); sessionErr == nil {
			linkedUser, err := api.auth.LinkMAL(r.Context(), existing.ID, code, payload.Verifier)
			if err != nil {
				api.writeMALLinkError(w, err)
				return
			}
			if err := api.setSessionCookie(w, r, linkedUser); err != nil {
				api.logError("auth", "failed to refresh session cookie after MAL link", "username", linkedUser.Username, "user_id", linkedUser.ID, "err", err)
				writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update session: %v", err))
				return
			}

			clearCookie(w, oauthCookieName, "/api/auth/mal")
			api.logInfo("auth", "MAL account linked to native user", "username", linkedUser.Username, "user_id", linkedUser.ID)
			http.Redirect(w, r, api.frontendRedirectURL(), http.StatusFound)
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

func (api *HTTPAPI) disconnectMALHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		updated, err := api.auth.UnlinkMAL(r.Context(), user.ID)
		if err != nil {
			api.logError("auth", "failed to disconnect MAL", "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to disconnect MAL: %v", err))
			return
		}

		if err := api.setSessionCookie(w, r, updated); err != nil {
			api.logError("auth", "failed to refresh session cookie after MAL disconnect", "user_id", updated.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update session: %v", err))
			return
		}

		api.logInfo("auth", "MAL account disconnected", "username", updated.Username, "user_id", updated.ID)
		writeJSON(w, http.StatusOK, MeResponse{Authenticated: true, User: toUserSummary(updated)})
	}
}

// writeMALLinkError maps MAL-link failures: a MAL account already linked to
// another user is a 409 conflict; remaining failures reuse the standalone MAL
// login error mapping (exchange / profile-fetch / persistence).
func (api *HTTPAPI) writeMALLinkError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrMALAlreadyLinked) {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	api.writeCompleteMALLoginError(w, err)
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
			User:          toUserSummary(user),
		})
	}
}

func (api *HTTPAPI) registerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid JSON body")
			return
		}

		user, err := api.auth.Register(r.Context(), req.Email, req.Username, req.Password)
		if err != nil {
			api.writeAuthCredentialError(w, "register", err)
			return
		}

		if err := api.setSessionCookie(w, r, user); err != nil {
			api.logError("auth", "failed to set session cookie after register", "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create session: %v", err))
			return
		}

		api.logInfo("auth", "native account registered", "username", user.Username, "user_id", user.ID)
		writeJSON(w, http.StatusCreated, MeResponse{Authenticated: true, User: toUserSummary(user)})
	}
}

func (api *HTTPAPI) loginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "Invalid JSON body")
			return
		}

		user, err := api.auth.Authenticate(r.Context(), req.Email, req.Password)
		if err != nil {
			api.writeAuthCredentialError(w, "login", err)
			return
		}

		if err := api.setSessionCookie(w, r, user); err != nil {
			api.logError("auth", "failed to set session cookie after login", "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create session: %v", err))
			return
		}

		api.logInfo("auth", "native account signed in", "username", user.Username, "user_id", user.ID)
		writeJSON(w, http.StatusOK, MeResponse{Authenticated: true, User: toUserSummary(user)})
	}
}

// writeAuthCredentialError maps register/login domain errors to HTTP statuses:
// validation -> 400, invalid credentials -> 401, uniqueness conflicts -> 409.
func (api *HTTPAPI) writeAuthCredentialError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidEmail),
		errors.Is(err, domain.ErrInvalidUsername),
		errors.Is(err, domain.ErrWeakPassword):
		writeAPIError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, usecase.ErrInvalidCredentials):
		writeAPIError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, domain.ErrEmailTaken),
		errors.Is(err, domain.ErrUsernameTaken):
		writeAPIError(w, http.StatusConflict, err.Error())
	default:
		api.logError("auth", "auth credential operation failed", "op", op, "err", err)
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to %s: %v", op, err))
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
