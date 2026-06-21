package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

type MALToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (token *MALToken) IsValid(now time.Time) bool {
	return token != nil && token.AccessToken != "" && now.Before(token.ExpiresAt)
}

type User struct {
	ID        int64
	MALUserID int64
	Username  string
	// Email is set only for native accounts (email + password registration).
	Email string
}

// MALLinked reports whether this user has a connected MAL account.
func (user User) MALLinked() bool {
	return user.MALUserID > 0
}

type MALUserProfile struct {
	ID       int64
	Username string
}

// Native account credential rules. UsernameMaxLength / pattern intentionally
// match the public MAL username rules so a native account can also be browsed
// through the public /api/public/anime/{username} route.
const (
	UsernameMinLength = 2
	UsernameMaxLength = 32
	EmailMaxLength    = 254
	PasswordMinLength = 8
	// bcrypt silently truncates input beyond 72 bytes, so reject longer
	// passwords instead of hashing a silently shortened value.
	PasswordMaxBytes = 72
)

var (
	ErrInvalidEmail    = errors.New("invalid email address")
	ErrInvalidUsername = errors.New("username must be 2-32 letters, numbers, underscores, or hyphens")
	ErrWeakPassword    = errors.New("password must be between 8 and 72 bytes")
)

// Persistence conflict errors. The auth repository returns these so use cases
// and the HTTP layer can map them to specific responses without inspecting
// driver-level constraint details.
var (
	ErrEmailTaken       = errors.New("email is already registered")
	ErrUsernameTaken    = errors.New("username is already taken")
	ErrMALAlreadyLinked = errors.New("this MAL account is already linked to another user")
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	// Pragmatic email shape check: a single @ with non-empty, dot-containing
	// host. Deliverability is not verified (MVP has no email sending).
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// NormalizeEmail trims surrounding spaces and lowercases the address so login
// lookups and the partial unique index stay case-insensitive.
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// NormalizeUsername trims surrounding spaces. Case is preserved for display but
// uniqueness is enforced case-insensitively by the database index.
func NormalizeUsername(raw string) string {
	return strings.TrimSpace(raw)
}

func ValidateEmail(email string) error {
	if email == "" || len(email) > EmailMaxLength || !emailPattern.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

func ValidateUsername(username string) error {
	if len(username) < UsernameMinLength || len(username) > UsernameMaxLength || !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < PasswordMinLength || len(password) > PasswordMaxBytes {
		return ErrWeakPassword
	}
	return nil
}
