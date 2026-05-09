package postgres

import (
	"context"
	"database/sql"
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
