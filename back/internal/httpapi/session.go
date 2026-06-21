package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"test/internal/domain"
)

const (
	sessionCookieName = "mal_session"
	oauthCookieName   = "mal_oauth"
	sessionMaxAge     = 30 * 24 * time.Hour
	oauthMaxAge       = 5 * time.Minute
)

var ErrUnauthenticated = errors.New("authentication required")

type signedSessionPayload struct {
	UserID    int64  `json:"uid"`
	MALUserID int64  `json:"mal_user_id,omitempty"`
	Username  string `json:"username"`
	Email     string `json:"email,omitempty"`
	ExpiresAt int64  `json:"exp"`
}

type signedOAuthPayload struct {
	State     string `json:"state"`
	Verifier  string `json:"verifier"`
	ExpiresAt int64  `json:"exp"`
}

func sessionSecret(config Config) []byte {
	secret := firstNonEmpty(config.SessionSecret, config.ClientSecret, config.ClientID)
	if secret == "" {
		secret = "mal-local-dev-session-secret"
	}
	return []byte(secret)
}

func (api *HTTPAPI) signCookiePayload(value any) (string, error) {
	return signCookiePayload(api.config, value)
}

func signCookiePayload(config Config, value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, sessionSecret(config))
	_, _ = mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encodedPayload + "." + signature, nil
}

func (api *HTTPAPI) verifyCookiePayload(raw string, value any) error {
	return verifyCookiePayload(api.config, raw, value)
}

func verifyCookiePayload(config Config, raw string, value any) error {
	encodedPayload, signature, ok := strings.Cut(raw, ".")
	if !ok || encodedPayload == "" || signature == "" {
		return ErrUnauthenticated
	}

	mac := hmac.New(sha256.New, sessionSecret(config))
	_, _ = mac.Write([]byte(encodedPayload))
	wantSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(wantSignature)) {
		return ErrUnauthenticated
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return ErrUnauthenticated
	}
	if err := json.Unmarshal(payload, value); err != nil {
		return ErrUnauthenticated
	}

	return nil
}

func (api *HTTPAPI) currentUserFromRequest(r *http.Request) (domain.User, error) {
	return currentUserFromRequest(api.config, r)
}

func currentUserFromRequest(config Config, r *http.Request) (domain.User, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return domain.User{}, ErrUnauthenticated
	}

	var payload signedSessionPayload
	if err := verifyCookiePayload(config, cookie.Value, &payload); err != nil {
		return domain.User{}, err
	}
	if payload.UserID <= 0 || strings.TrimSpace(payload.Username) == "" {
		return domain.User{}, ErrUnauthenticated
	}
	if time.Now().Unix() >= payload.ExpiresAt {
		return domain.User{}, ErrUnauthenticated
	}

	return domain.User{ID: payload.UserID, MALUserID: payload.MALUserID, Username: payload.Username, Email: payload.Email}, nil
}

func (api *HTTPAPI) setSessionCookie(w http.ResponseWriter, r *http.Request, user domain.User) error {
	return setSessionCookie(api.config, w, r, user)
}

func setSessionCookie(config Config, w http.ResponseWriter, r *http.Request, user domain.User) error {
	if user.ID <= 0 || strings.TrimSpace(user.Username) == "" {
		return fmt.Errorf("cannot create session for invalid user")
	}

	value, err := signCookiePayload(config, signedSessionPayload{
		UserID:    user.ID,
		MALUserID: user.MALUserID,
		Username:  user.Username,
		Email:     user.Email,
		ExpiresAt: time.Now().Add(sessionMaxAge).Unix(),
	})
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func requestIsSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
