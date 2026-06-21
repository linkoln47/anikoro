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

func (repo *AuthRepository) UserByUsername(ctx context.Context, username string) (domain.User, bool, error) {
	ctx = ensureContext(ctx)
	username = strings.TrimSpace(username)
	if username == "" {
		return domain.User{}, false, errors.New("username cannot be empty")
	}

	var user domain.User
	var malUserID sql.NullInt64
	err := repo.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, p.mal_user_id
		FROM `+UsersTableName+` u
		LEFT JOIN `+MALProfilesTableName+` p ON p.user_id = u.id
		WHERE LOWER(u.username) = LOWER($1)
		ORDER BY u.id
		LIMIT 1
	`, username).Scan(&user.ID, &user.Username, &malUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, false, nil
		}
		return domain.User{}, false, err
	}

	user.MALUserID = malUserID.Int64
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
		SELECT u.id, p.mal_user_id, u.username, u.email, u.password_hash
		FROM `+UsersTableName+` u
		LEFT JOIN `+MALProfilesTableName+` p ON p.user_id = u.id
		WHERE LOWER(u.email) = LOWER($1)
		  AND u.password_hash IS NOT NULL
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

// AttachMALProfile creates the MAL identity row for an existing native user.
// The MAL username is captured from the OAuth profile (FetchCurrentUser). It
// returns ErrMALAlreadyLinked when the MAL account belongs to another user and
// ErrMALProfileExists when this user already linked a MAL account.
func (repo *AuthRepository) AttachMALProfile(ctx context.Context, userID int64, profile domain.MALUserProfile) (domain.MALProfile, domain.User, error) {
	ctx = ensureContext(ctx)
	profile.Username = strings.TrimSpace(profile.Username)
	if userID <= 0 {
		return domain.MALProfile{}, domain.User{}, errors.New("user_id must be positive")
	}
	if profile.ID <= 0 {
		return domain.MALProfile{}, domain.User{}, errors.New("mal_user_id must be positive")
	}
	if profile.Username == "" {
		return domain.MALProfile{}, domain.User{}, errors.New("username cannot be empty")
	}

	var malProfile domain.MALProfile
	var user domain.User
	err := WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		// Reject linking a MAL account that already belongs to a different user
		// with a clear error (rather than relying on the unique violation).
		var ownerID int64
		err := tx.QueryRowContext(ctx, `
			SELECT user_id FROM `+MALProfilesTableName+`
			WHERE mal_user_id = $1
		`, profile.ID).Scan(&ownerID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && ownerID != userID {
			return domain.ErrMALAlreadyLinked
		}

		err = tx.QueryRowContext(ctx, `
			INSERT INTO `+MALProfilesTableName+` (
				user_id,
				mal_user_id,
				mal_username,
				created_at,
				updated_at
			) VALUES ($1, $2, $3, NOW(), NOW())
			RETURNING id, user_id, mal_user_id, mal_username
		`, userID, profile.ID, profile.Username).Scan(
			&malProfile.ID, &malProfile.UserID, &malProfile.MALUserID, &malProfile.Username,
		)
		if err != nil {
			return mapMALProfileUniqueViolation(err)
		}

		var storedEmail sql.NullString
		err = tx.QueryRowContext(ctx, `
			SELECT id, username, email
			FROM `+UsersTableName+`
			WHERE id = $1
		`, userID).Scan(&user.ID, &user.Username, &storedEmail)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("user %d not found", userID)
			}
			return err
		}

		user.Email = storedEmail.String
		user.MALUserID = malProfile.MALUserID
		return nil
	})
	if err != nil {
		return domain.MALProfile{}, domain.User{}, err
	}

	return malProfile, user, nil
}

// UnlinkMALProfile deletes the user's MAL profile. The stored MAL token is
// removed by cascade. The synced anime snapshot (user_anime_items, keyed by
// user_id) is intentionally left untouched.
func (repo *AuthRepository) UnlinkMALProfile(ctx context.Context, userID int64) (domain.User, error) {
	ctx = ensureContext(ctx)
	if userID <= 0 {
		return domain.User{}, errors.New("user_id must be positive")
	}

	var user domain.User
	err := WithTx(ctx, repo.db, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM `+MALProfilesTableName+`
			WHERE user_id = $1
		`, userID); err != nil {
			return err
		}

		var storedEmail sql.NullString
		err := tx.QueryRowContext(ctx, `
			SELECT id, username, email
			FROM `+UsersTableName+`
			WHERE id = $1
		`, userID).Scan(&user.ID, &user.Username, &storedEmail)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("user %d not found", userID)
			}
			return err
		}

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
		SELECT t.access_token, t.token_type, t.expires_at
		FROM `+MALTokensTable+` t
		JOIN `+MALProfilesTableName+` p ON p.id = t.mal_profile_id
		WHERE p.user_id = $1
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

func (repo *AuthRepository) SaveToken(ctx context.Context, malProfileID int64, token domain.MALToken) error {
	ctx = ensureContext(ctx)
	if malProfileID <= 0 {
		return errors.New("mal_profile_id must be positive")
	}
	if token.AccessToken == "" {
		return errors.New("token cannot be empty")
	}

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO `+MALTokensTable+` (
			mal_profile_id,
			access_token,
			refresh_token,
			token_type,
			expires_at,
			created_at,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		ON CONFLICT (mal_profile_id) DO UPDATE
		SET access_token = EXCLUDED.access_token,
		    refresh_token = EXCLUDED.refresh_token,
		    token_type = EXCLUDED.token_type,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = NOW()
	`, malProfileID, token.AccessToken, NullableString(token.RefreshToken), token.TokenType, token.ExpiresAt.UTC())
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
	default:
		return err
	}
}

// mapMALProfileUniqueViolation translates a unique violation on mal_profiles
// into the matching domain conflict error.
func mapMALProfileUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgUniqueViolationCode {
		return err
	}

	switch pgErr.ConstraintName {
	case "mal_profiles_mal_user_id_idx":
		return domain.ErrMALAlreadyLinked
	case "mal_profiles_user_id_idx":
		return domain.ErrMALProfileExists
	default:
		return err
	}
}
