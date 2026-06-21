package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeEmailLowercasesAndTrims(t *testing.T) {
	if got := NormalizeEmail("  User@Example.COM "); got != "user@example.com" {
		t.Fatalf("NormalizeEmail = %q, want %q", got, "user@example.com")
	}
}

func TestValidateEmail(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  error
	}{
		{"valid", "user@example.com", nil},
		{"empty", "", ErrInvalidEmail},
		{"no at", "userexample.com", ErrInvalidEmail},
		{"no dot host", "user@example", ErrInvalidEmail},
		{"spaces", "user @example.com", ErrInvalidEmail},
		{"too long", strings.Repeat("a", 250) + "@b.co", ErrInvalidEmail},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateEmail(tc.email); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateEmail(%q) = %v, want %v", tc.email, got, tc.want)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	cases := []struct {
		name     string
		username string
		want     error
	}{
		{"valid", "Alice_99", nil},
		{"hyphen", "a-b", nil},
		{"too short", "a", ErrInvalidUsername},
		{"too long", strings.Repeat("a", UsernameMaxLength+1), ErrInvalidUsername},
		{"space", "bad name", ErrInvalidUsername},
		{"symbol", "bad@name", ErrInvalidUsername},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateUsername(tc.username); !errors.Is(got, tc.want) {
				t.Fatalf("ValidateUsername(%q) = %v, want %v", tc.username, got, tc.want)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		want     error
	}{
		{"valid", "supersecret", nil},
		{"min boundary", strings.Repeat("a", PasswordMinLength), nil},
		{"too short", strings.Repeat("a", PasswordMinLength-1), ErrWeakPassword},
		{"too long", strings.Repeat("a", PasswordMaxBytes+1), ErrWeakPassword},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidatePassword(tc.password); !errors.Is(got, tc.want) {
				t.Fatalf("ValidatePassword(%q) = %v, want %v", tc.password, got, tc.want)
			}
		})
	}
}

func TestUserMALLinked(t *testing.T) {
	if (User{MALUserID: 0}).MALLinked() {
		t.Fatal("user without mal_user_id should not be linked")
	}
	if !(User{MALUserID: 5}).MALLinked() {
		t.Fatal("user with mal_user_id should be linked")
	}
}
