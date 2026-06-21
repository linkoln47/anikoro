package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"test/internal/domain"
)

func TestCreateUserWithPasswordAndCredentialLookup(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	created, err := repo.CreateUserWithPassword(ctx, "Alice@Example.com", "Alice", "hash-1")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}
	if created.ID <= 0 || created.Username != "Alice" || created.Email != "alice@example.com" {
		t.Fatalf("unexpected created user: %+v", created)
	}

	user, hash, found, err := repo.UserCredentialsByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("UserCredentialsByEmail returned error: %v", err)
	}
	if !found || user.ID != created.ID || hash != "hash-1" {
		t.Fatalf("unexpected credentials lookup: found=%v user=%+v hash=%q", found, user, hash)
	}

	// Email uniqueness is case-insensitive.
	if _, err := repo.CreateUserWithPassword(ctx, "ALICE@example.com", "Alice2", "hash-2"); !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}

	// Username uniqueness is case-insensitive.
	if _, err := repo.CreateUserWithPassword(ctx, "other@example.com", "alice", "hash-3"); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestAttachMALProfileLinksToNativeUser(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	native, err := repo.CreateUserWithPassword(ctx, "alice@example.com", "Alice", "hash-1")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}

	profile, linked, err := repo.AttachMALProfile(ctx, native.ID, domain.MALUserProfile{ID: 999, Username: "AliceMAL"})
	if err != nil {
		t.Fatalf("AttachMALProfile returned error: %v", err)
	}
	if profile.ID <= 0 || profile.UserID != native.ID || profile.MALUserID != 999 || profile.Username != "AliceMAL" {
		t.Fatalf("unexpected created profile: %+v", profile)
	}
	if linked.ID != native.ID || linked.MALUserID != 999 {
		t.Fatalf("expected MAL linked to native row, got %+v", linked)
	}
	if linked.Username != "Alice" {
		t.Fatalf("expected native username preserved, got %q", linked.Username)
	}

	// The same user cannot link a second MAL account (1:1).
	if _, _, err := repo.AttachMALProfile(ctx, native.ID, domain.MALUserProfile{ID: 1000, Username: "AliceAlt"}); !errors.Is(err, domain.ErrMALProfileExists) {
		t.Fatalf("expected ErrMALProfileExists, got %v", err)
	}

	// A second native account cannot claim the same MAL identity.
	other, err := repo.CreateUserWithPassword(ctx, "bob@example.com", "Bob", "hash-2")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}
	if _, _, err := repo.AttachMALProfile(ctx, other.ID, domain.MALUserProfile{ID: 999, Username: "AliceMAL"}); !errors.Is(err, domain.ErrMALAlreadyLinked) {
		t.Fatalf("expected ErrMALAlreadyLinked, got %v", err)
	}
}

func TestUnlinkMALProfileDeletesProfileAndTokenKeepsAccount(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	native, err := repo.CreateUserWithPassword(ctx, "alice@example.com", "Alice", "hash-1")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}
	profile, _, err := repo.AttachMALProfile(ctx, native.ID, domain.MALUserProfile{ID: 999, Username: "AliceMAL"})
	if err != nil {
		t.Fatalf("AttachMALProfile returned error: %v", err)
	}
	if err := repo.SaveToken(ctx, profile.ID, domain.MALToken{
		AccessToken: "access-1",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveToken returned error: %v", err)
	}

	unlinked, err := repo.UnlinkMALProfile(ctx, native.ID)
	if err != nil {
		t.Fatalf("UnlinkMALProfile returned error: %v", err)
	}
	if unlinked.ID != native.ID || unlinked.MALUserID != 0 {
		t.Fatalf("expected cleared mal link, got %+v", unlinked)
	}
	if unlinked.Username != "Alice" || unlinked.Email != "alice@example.com" {
		t.Fatalf("expected native identity preserved, got %+v", unlinked)
	}

	// The token (cascaded with the profile) is gone, but the account survives.
	if _, found, err := repo.LoadToken(ctx, native.ID); err != nil || found {
		t.Fatalf("expected no token after unlink, found=%v err=%v", found, err)
	}
	if _, _, found, err := repo.UserCredentialsByEmail(ctx, "alice@example.com"); err != nil || !found {
		t.Fatalf("expected account to survive unlink, found=%v err=%v", found, err)
	}
}

func openAuthRepositoryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	databaseURL := firstNonEmpty(strings.TrimSpace(os.Getenv("MAL_TEST_DATABASE_URL")), strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")))
	if databaseURL == "" {
		t.Skip("set MAL_TEST_DATABASE_URL or TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	schemaName := fmt.Sprintf("auth_repository_test_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		_ = db.Close()
		t.Fatalf("create test schema: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`)
		_ = db.Close()
	})

	if _, err := db.ExecContext(ctx, `SET search_path TO `+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	schemaSQL, err := os.ReadFile("../../../schema.sql")
	if err != nil {
		t.Fatalf("read schema.sql: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(schemaSQL)); err != nil {
		t.Fatalf("apply schema.sql: %v", err)
	}

	return db
}
