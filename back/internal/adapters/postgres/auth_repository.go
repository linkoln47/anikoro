package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"test/internal/domain"
	"test/internal/ports"
)

// pgUniqueViolationCode is the PostgreSQL SQLSTATE for a unique constraint or
// unique index violation.
const pgUniqueViolationCode = "23505"

type AuthRepository struct {
	db *sql.DB
}

var _ ports.AuthRepository = (*AuthRepository)(nil)

func NewAuthRepository(db *sql.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (repo *AuthRepository) UpsertMALUser(ctx context.Context, profile domain.MALUserProfile) (domain.User, error) {
	ctx = ensureContext(ctx)
	profile.Username = strings.TrimSpace(profile.Username)
	if profile.ID <= 0 {
		return domain.User{}, errors.New("mal_user_id must be positive")
	}
	if profile.Username == "" {
		return domain.User{}, errors.New("username cannot be empty")
	}

	var user domain.User
	err := WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `
			SELECT id, mal_user_id, username
			FROM `+UsersTableName+`
			WHERE mal_user_id = $1
		`, profile.ID).Scan(&user.ID, &user.MALUserID, &user.Username)
		if err == nil {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM `+UsersTableName+`
				WHERE mal_user_id IS NULL
				  AND LOWER(username) = LOWER($1)
				  AND id <> $2
			`, profile.Username, user.ID); err != nil {
				return err
			}

			return tx.QueryRowContext(ctx, `
				UPDATE `+UsersTableName+`
				SET username = $2,
				    updated_at = NOW()
				WHERE id = $1
				RETURNING id, mal_user_id, username
			`, user.ID, profile.Username).Scan(&user.ID, &user.MALUserID, &user.Username)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		err = tx.QueryRowContext(ctx, `
			UPDATE `+UsersTableName+`
			SET mal_user_id = $1,
			    username = $2,
			    updated_at = NOW()
			WHERE mal_user_id IS NULL
			  AND LOWER(username) = LOWER($2)
			RETURNING id, mal_user_id, username
		`, profile.ID, profile.Username).Scan(&user.ID, &user.MALUserID, &user.Username)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		return tx.QueryRowContext(ctx, `
			INSERT INTO `+UsersTableName+` (
				mal_user_id,
				username,
				created_at,
				updated_at
			) VALUES ($1, $2, NOW(), NOW())
			ON CONFLICT (mal_user_id) DO UPDATE
			SET username = EXCLUDED.username,
			    updated_at = NOW()
			RETURNING id, mal_user_id, username
		`, profile.ID, profile.Username).Scan(&user.ID, &user.MALUserID, &user.Username)
	})
	if err != nil {
		return domain.User{}, err
	}

	return user, nil
}

func (repo *AuthRepository) UpsertUserByPublicUsername(ctx context.Context, username string) (domain.User, error) {
	ctx = ensureContext(ctx)
	username = strings.TrimSpace(username)
	if username == "" {
		return domain.User{}, errors.New("username cannot be empty")
	}

	var user domain.User
	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO `+UsersTableName+` (
			username,
			created_at,
			updated_at
		) VALUES ($1, NOW(), NOW())
		ON CONFLICT ((LOWER(username))) DO UPDATE
		SET username = EXCLUDED.username,
		    updated_at = NOW()
		RETURNING id, username
	`, username).Scan(&user.ID, &user.Username)
	if err != nil {
		return domain.User{}, err
	}

	return user, nil
}

func (repo *AuthRepository) UserByUsername(ctx context.Context, username string) (domain.User, bool, error) {
	ctx = ensureContext(ctx)
	username = strings.TrimSpace(username)
	if username == "" {
		return domain.User{}, false, errors.New("username cannot be empty")
	}

	var user domain.User
	err := repo.db.QueryRowContext(ctx, `
		SELECT id, username
		FROM `+UsersTableName+`
		WHERE LOWER(username) = LOWER($1)
		ORDER BY id
		LIMIT 1
	`, username).Scan(&user.ID, &user.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, false, nil
		}
		return domain.User{}, false, err
	}

	return user, true, nil
}

func (repo *AuthRepository) CreateUserWithPassword(ctx context.Context, email, username, passwordHash string) (domain.User, error) {
	ctx = ensureContext(ctx)
	email = domain.NormalizeEmail(email)
	username = domain.NormalizeUsername(username)
	if email == "" {
		return domain.User{}, errors.New("email cannot be empty")
	}
	if username == "" {
		return domain.User{}, errors.New("username cannot be empty")
	}
	if passwordHash == "" {
		return domain.User{}, errors.New("password hash cannot be empty")
	}

	var user domain.User
	var storedEmail sql.NullString
	err := repo.db.QueryRowContext(ctx, `
		INSERT INTO `+UsersTableName+` (
			username,
			email,
			password_hash,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id, username, email
	`, username, email, passwordHash).Scan(&user.ID, &user.Username, &storedEmail)
	if err != nil {
		return domain.User{}, mapUserUniqueViolation(err)
	}

	user.Email = storedEmail.String
	return user, nil
}

func (repo *AuthRepository) UserCredentialsByEmail(ctx context.Context, email string) (domain.User, string, bool, error) {
	ctx = ensureContext(ctx)
	email = domain.NormalizeEmail(email)
	if email == "" {
		return domain.User{}, "", false, errors.New("email cannot be empty")
	}

	var user domain.User
	var malUserID sql.NullInt64
	var storedEmail sql.NullString
	var passwordHash sql.NullString
	err := repo.db.QueryRowContext(ctx, `
		SELECT id, mal_user_id, username, email, password_hash
		FROM `+UsersTableName+`
		WHERE LOWER(email) = LOWER($1)
		  AND password_hash IS NOT NULL
		LIMIT 1
	`, email).Scan(&user.ID, &malUserID, &user.Username, &storedEmail, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, "", false, nil
		}
		return domain.User{}, "", false, err
	}

	user.MALUserID = malUserID.Int64
	user.Email = storedEmail.String
	return user, passwordHash.String, true, nil
}

func (repo *AuthRepository) AttachMALIdentity(ctx context.Context, userID int64, profile domain.MALUserProfile) (domain.User, error) {
	ctx = ensureContext(ctx)
	if userID <= 0 {
		return domain.User{}, errors.New("user_id must be positive")
	}
	if profile.ID <= 0 {
		return domain.User{}, errors.New("mal_user_id must be positive")
	}

	var user domain.User
	err := WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		var ownerID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM `+UsersTableName+`
			WHERE mal_user_id = $1
		`, profile.ID).Scan(&ownerID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && ownerID != userID {
			return domain.ErrMALAlreadyLinked
		}

		var malUserID sql.NullInt64
		var storedEmail sql.NullString
		err = tx.QueryRowContext(ctx, `
			UPDATE `+UsersTableName+`
			SET mal_user_id = $2,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING id, mal_user_id, username, email
		`, userID, profile.ID).Scan(&user.ID, &malUserID, &user.Username, &storedEmail)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("user %d not found", userID)
			}
			return mapUserUniqueViolation(err)
		}

		user.MALUserID = malUserID.Int64
		user.Email = storedEmail.String
		return nil
	})
	if err != nil {
		return domain.User{}, err
	}

	return user, nil
}

func (repo *AuthRepository) UnlinkMALAccount(ctx context.Context, userID int64) (domain.User, error) {
	ctx = ensureContext(ctx)
	if userID <= 0 {
		return domain.User{}, errors.New("user_id must be positive")
	}

	var user domain.User
	err := WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		// Drop the OAuth token but keep user_anime_items: the synced snapshot
		// stays even after the MAL link is removed.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM `+MALTokensTable+`
			WHERE user_id = $1
		`, userID); err != nil {
			return err
		}

		var malUserID sql.NullInt64
		var storedEmail sql.NullString
		err := tx.QueryRowContext(ctx, `
			UPDATE `+UsersTableName+`
			SET mal_user_id = NULL,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING id, mal_user_id, username, email
		`, userID).Scan(&user.ID, &malUserID, &user.Username, &storedEmail)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("user %d not found", userID)
			}
			return err
		}

		user.MALUserID = malUserID.Int64
		user.Email = storedEmail.String
		return nil
	})
	if err != nil {
		return domain.User{}, err
	}

	return user, nil
}

func (repo *AuthRepository) LoadToken(ctx context.Context, userID int64) (domain.MALToken, bool, error) {
	ctx = ensureContext(ctx)
	var token domain.MALToken

	err := repo.db.QueryRowContext(ctx, `
		SELECT access_token, token_type, expires_at
		FROM `+MALTokensTable+`
		WHERE user_id = $1
	`, userID).Scan(&token.AccessToken, &token.TokenType, &token.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.MALToken{}, false, nil
		}
		return domain.MALToken{}, false, err
	}
	if token.AccessToken == "" {
		return domain.MALToken{}, false, errors.New("empty access_token in database")
	}

	return token, true, nil
}

func (repo *AuthRepository) SaveToken(ctx context.Context, userID int64, token domain.MALToken) error {
	ctx = ensureContext(ctx)
	if token.AccessToken == "" {
		return errors.New("token cannot be empty")
	}

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO `+MALTokensTable+` (
			user_id,
			access_token,
			refresh_token,
			token_type,
			expires_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET access_token = EXCLUDED.access_token,
		    refresh_token = EXCLUDED.refresh_token,
		    token_type = EXCLUDED.token_type,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = NOW()
	`, userID, token.AccessToken, NullableString(token.RefreshToken), token.TokenType, token.ExpiresAt.UTC())
	return err
}

// mapUserUniqueViolation translates a PostgreSQL unique violation on the users
// table into the matching domain conflict error, based on the violated index.
// Non-violation errors pass through unchanged.
func mapUserUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolationCode {
		return err
	}

	switch pgErr.ConstraintName {
	case "users_email_lower_idx":
		return domain.ErrEmailTaken
	case "users_username_lower_idx":
		return domain.ErrUsernameTaken
	case "users_mal_user_id_idx":
		return domain.ErrMALAlreadyLinked
	default:
		return err
	}
}
