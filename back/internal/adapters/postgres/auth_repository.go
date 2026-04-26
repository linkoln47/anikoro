package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"test/internal/domain"
)

type AuthRepository struct {
	db *sql.DB
}

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

func (repo *AuthRepository) UpsertPublicUser(ctx context.Context, username string) (domain.User, error) {
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
