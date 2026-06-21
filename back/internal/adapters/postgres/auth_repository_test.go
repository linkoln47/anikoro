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

func TestAuthRepositoryReusesUsernameRowAcrossPublicAndOAuth(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	publicUser, err := repo.UpsertUserByPublicUsername(ctx, "foo")
	if err != nil {
		t.Fatalf("UpsertUserByPublicUsername returned error: %v", err)
	}
	if publicUser.ID <= 0 {
		t.Fatalf("expected public username upsert to create a user id, got %+v", publicUser)
	}
	if publicUser.MALUserID != 0 {
		t.Fatalf("public username upsert should not invent mal_user_id, got %+v", publicUser)
	}

	oauthUser, err := repo.UpsertMALUser(ctx, domain.MALUserProfile{
		ID:       12345,
		Username: "Foo",
	})
	if err != nil {
		t.Fatalf("UpsertMALUser returned error: %v", err)
	}
	if oauthUser.ID != publicUser.ID {
		t.Fatalf("expected OAuth login to attach to existing username row: public id=%d oauth id=%d", publicUser.ID, oauthUser.ID)
	}
	if oauthUser.MALUserID != 12345 {
		t.Fatalf("expected OAuth login to persist mal_user_id, got %+v", oauthUser)
	}

	publicAgain, err := repo.UpsertUserByPublicUsername(ctx, "foo")
	if err != nil {
		t.Fatalf("second UpsertUserByPublicUsername returned error: %v", err)
	}
	if publicAgain.ID != oauthUser.ID {
		t.Fatalf("expected later public sync to reuse OAuth-linked row: public id=%d oauth id=%d", publicAgain.ID, oauthUser.ID)
	}
}

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

func TestUserCredentialsByEmailIgnoresPasswordlessRows(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	// Public-sync rows have no email/password and must not be returned as
	// login-able accounts.
	if _, err := repo.UpsertUserByPublicUsername(ctx, "Ghost"); err != nil {
		t.Fatalf("UpsertUserByPublicUsername returned error: %v", err)
	}

	_, _, found, err := repo.UserCredentialsByEmail(ctx, "ghost@example.com")
	if err != nil {
		t.Fatalf("UserCredentialsByEmail returned error: %v", err)
	}
	if found {
		t.Fatal("expected no login-able account for a public-sync username")
	}
}

func TestAttachMALIdentityLinksWithoutOverwritingUsername(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	native, err := repo.CreateUserWithPassword(ctx, "alice@example.com", "Alice", "hash-1")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}

	linked, err := repo.AttachMALIdentity(ctx, native.ID, domain.MALUserProfile{ID: 999, Username: "AliceMAL"})
	if err != nil {
		t.Fatalf("AttachMALIdentity returned error: %v", err)
	}
	if linked.ID != native.ID || linked.MALUserID != 999 {
		t.Fatalf("expected MAL linked to native row, got %+v", linked)
	}
	if linked.Username != "Alice" {
		t.Fatalf("expected native username preserved, got %q", linked.Username)
	}

	// A second native account cannot claim the same MAL identity.
	other, err := repo.CreateUserWithPassword(ctx, "bob@example.com", "Bob", "hash-2")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}
	if _, err := repo.AttachMALIdentity(ctx, other.ID, domain.MALUserProfile{ID: 999, Username: "AliceMAL"}); !errors.Is(err, domain.ErrMALAlreadyLinked) {
		t.Fatalf("expected ErrMALAlreadyLinked, got %v", err)
	}
}

func TestUnlinkMALAccountClearsLinkAndToken(t *testing.T) {
	db := openAuthRepositoryTestDB(t)
	repo := NewAuthRepository(db)
	ctx := context.Background()

	native, err := repo.CreateUserWithPassword(ctx, "alice@example.com", "Alice", "hash-1")
	if err != nil {
		t.Fatalf("CreateUserWithPassword returned error: %v", err)
	}
	if _, err := repo.AttachMALIdentity(ctx, native.ID, domain.MALUserProfile{ID: 999, Username: "AliceMAL"}); err != nil {
		t.Fatalf("AttachMALIdentity returned error: %v", err)
	}
	if err := repo.SaveToken(ctx, native.ID, domain.MALToken{
		AccessToken: "access-1",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveToken returned error: %v", err)
	}

	unlinked, err := repo.UnlinkMALAccount(ctx, native.ID)
	if err != nil {
		t.Fatalf("UnlinkMALAccount returned error: %v", err)
	}
	if unlinked.ID != native.ID || unlinked.MALUserID != 0 {
		t.Fatalf("expected cleared mal link, got %+v", unlinked)
	}
	if unlinked.Username != "Alice" || unlinked.Email != "alice@example.com" {
		t.Fatalf("expected native identity preserved, got %+v", unlinked)
	}

	// The token row is gone, but the account still exists for password login.
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
