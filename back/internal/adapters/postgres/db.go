package postgres

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
	AnimeFranchisesTableName          = "anime_franchises"
	AnimeFranchiseMembersTableName    = "anime_franchise_members"
	UsersTableName                    = "users"
	MALTokensTable                    = "mal_tokens"
	TraversableAnimeRelationFilterSQL = "COALESCE(LOWER(relation_type), '') NOT IN ('character', 'other')"

	userScopeSetting = "app.user_id"
	defaultDBTimeout = 5 * time.Second
	defaultMaxOpenDB = 10
	defaultMaxIdleDB = 5
)

func OpenDB(databaseURL string) (*sql.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", databaseURL)
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

func WithTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
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

func WithUserTx(ctx context.Context, db *sql.DB, userID int64, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
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

func NullableDate(value string) any {
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

func NullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func BuildSQLPlaceholders(start, count int) string {
	if count <= 0 {
		return ""
	}

	placeholders := make([]string, 0, count)
	for i := 0; i < count; i++ {
		placeholders = append(placeholders, fmt.Sprintf("$%d", start+i))
	}

	return strings.Join(placeholders, ", ")
}

func IntsToAnySlice(ids []int) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return args
}

func Int64sToAnySlice(ids []int64) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return args
}

func UniquePositiveInt64s(ids []int64) []int64 {
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

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
