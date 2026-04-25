package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	animeFranchisesTableName          = "anime_franchises"
	animeFranchiseMembersTableName    = "anime_franchise_members"
	usersTableName                    = "users"
	malTokensTable                    = "mal_tokens"
	userScopeSetting                  = "app.user_id"
	defaultDBTimeout                  = 5 * time.Second
	defaultMaxOpenDB                  = 10
	defaultMaxIdleDB                  = 5
	traversableAnimeRelationFilterSQL = "COALESCE(LOWER(relation_type), '') NOT IN ('character', 'other')"
)

func openDB(cfg AppConfig) (*sql.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	db.SetMaxOpenConns(defaultMaxOpenDB)
	db.SetMaxIdleConns(defaultMaxIdleDB)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}

	return db, nil
}

func withTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	ctx = ensureContext(ctx)

	if db == nil {
		return fmt.Errorf("postgres repository requires a database handle")
	}

	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func withUserTx(ctx context.Context, db *sql.DB, userID int64, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	ctx = ensureContext(ctx)

	if db == nil {
		return fmt.Errorf("postgres repository requires a database handle")
	}
	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}

	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := setUserScope(ctx, tx, userID); err != nil {
		return fmt.Errorf("set user scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func setUserScope(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('`+userScopeSetting+`', $1, true)`, strconv.FormatInt(userID, 10))
	return err
}

func nullableDate(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		return parsed.Format("2006-01-02")
	}

	return nil
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func buildSQLPlaceholders(start, count int) string {
	if count <= 0 {
		return ""
	}

	placeholders := make([]string, 0, count)
	for i := 0; i < count; i++ {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+i))
	}

	return strings.Join(placeholders, ", ")
}

func intsToAnySlice(ids []int) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return args
}

func int64sToAnySlice(ids []int64) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return args
}

func uniquePositiveInt64s(ids []int64) []int64 {
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return unique
}
