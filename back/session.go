package main

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
	Username  string `json:"username"`
	ExpiresAt int64  `json:"exp"`
}

type signedOAuthPayload struct {
	State     string `json:"state"`
	Verifier  string `json:"verifier"`
	ExpiresAt int64  `json:"exp"`
}

func (a *App) sessionSecret() []byte {
	secret := firstNonEmpty(a.Config.SessionSecret, a.Config.ClientSecret, a.Config.ClientID)
	if secret == "" {
		secret = "mal-local-dev-session-secret"
	}
	return []byte(secret)
}

func (a *App) signCookiePayload(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, a.sessionSecret())
	_, _ = mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return encodedPayload + "." + signature, nil
}

func (a *App) verifyCookiePayload(raw string, value any) error {
	encodedPayload, signature, ok := strings.Cut(raw, ".")
	if !ok || encodedPayload == "" || signature == "" {
		return ErrUnauthenticated
	}

	mac := hmac.New(sha256.New, a.sessionSecret())
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

func (a *App) currentUserFromRequest(r *http.Request) (User, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return User{}, ErrUnauthenticated
	}

	var payload signedSessionPayload
	if err := a.verifyCookiePayload(cookie.Value, &payload); err != nil {
		return User{}, err
	}
	if payload.UserID <= 0 || strings.TrimSpace(payload.Username) == "" {
		return User{}, ErrUnauthenticated
	}
	if time.Now().Unix() >= payload.ExpiresAt {
		return User{}, ErrUnauthenticated
	}

	return User{ID: payload.UserID, Username: payload.Username}, nil
}

func (a *App) setSessionCookie(w http.ResponseWriter, r *http.Request, user User) error {
	if user.ID <= 0 || strings.TrimSpace(user.Username) == "" {
		return fmt.Errorf("cannot create session for invalid user")
	}

	value, err := a.signCookiePayload(signedSessionPayload{
		UserID:    user.ID,
		Username:  user.Username,
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
