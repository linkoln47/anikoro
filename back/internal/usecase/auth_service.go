package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

var (
	ErrNoValidToken              = errors.New("no token stored for this user; sign in with MAL")
	ErrTokenExpired              = errors.New("token expired; sign in with MAL again")
	ErrUserNotFound              = errors.New("user not found")
	ErrInvalidCredentials        = errors.New("invalid email or password")
	ErrMALTokenExchangeFailed    = errors.New("failed to exchange MAL authorization code")
	ErrMALCurrentUserFetchFailed = errors.New("failed to fetch MAL current user")
	ErrAuthUserSaveFailed        = errors.New("failed to save user")
	ErrAuthTokenSaveFailed       = errors.New("failed to save token")
)

type AuthService struct {
	repo        ports.AuthRepository
	hasher      ports.PasswordHasher
	oauth       ports.MALOAuthClient
	oauthConfig ports.MALOAuthConfig
}

type AuthServiceDependencies struct {
	Repo        ports.AuthRepository
	Hasher      ports.PasswordHasher
	OAuth       ports.MALOAuthClient
	OAuthConfig ports.MALOAuthConfig
}

func NewAuthService(deps AuthServiceDependencies) *AuthService {
	return &AuthService{
		repo:        deps.Repo,
		hasher:      deps.Hasher,
		oauth:       deps.OAuth,
		oauthConfig: deps.OAuthConfig,
	}
}

// Register creates a native account from an email, public username, and
// password. It validates inputs against domain rules, hashes the password, and
// persists the user. Conflicts surface as domain.ErrEmailTaken /
// domain.ErrUsernameTaken.
func (service *AuthService) Register(ctx context.Context, email, username, password string) (domain.User, error) {
	ctx = ensureContext(ctx)

	email = domain.NormalizeEmail(email)
	username = domain.NormalizeUsername(username)
	if err := domain.ValidateEmail(email); err != nil {
		return domain.User{}, err
	}
	if err := domain.ValidateUsername(username); err != nil {
		return domain.User{}, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return domain.User{}, err
	}

	hash, err := service.hasher.Hash(password)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := service.repo.CreateUserWithPassword(ctx, email, username, hash)
	if err != nil {
		return domain.User{}, err
	}

	return user, nil
}

// Authenticate verifies an email + password pair and returns the matching
// native account. A missing account and a wrong password both collapse into
// ErrInvalidCredentials so callers cannot probe which emails exist.
func (service *AuthService) Authenticate(ctx context.Context, email, password string) (domain.User, error) {
	ctx = ensureContext(ctx)

	email = domain.NormalizeEmail(email)
	if email == "" || password == "" {
		return domain.User{}, ErrInvalidCredentials
	}

	user, hash, found, err := service.repo.UserCredentialsByEmail(ctx, email)
	if err != nil {
		return domain.User{}, err
	}
	if !found {
		return domain.User{}, ErrInvalidCredentials
	}

	if err := service.hasher.Compare(hash, password); err != nil {
		if errors.Is(err, ports.ErrPasswordMismatch) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, err
	}

	return user, nil
}

// LinkMAL completes the MAL OAuth exchange and attaches the resulting MAL
// identity and token to an already authenticated native account. The anime
// snapshot stays keyed by the same user_id, so sync keeps working unchanged.
func (service *AuthService) LinkMAL(ctx context.Context, userID int64, code, verifier string) (domain.User, error) {
	ctx = ensureContext(ctx)
	if userID <= 0 {
		return domain.User{}, errors.New("user_id must be positive")
	}

	token, err := service.oauth.ExchangeCodeForToken(ctx, service.oauthConfig, code, verifier)
	if err != nil {
		return domain.User{}, fmt.Errorf("%w: %w", ErrMALTokenExchangeFailed, err)
	}
	if token == nil || token.AccessToken == "" {
		return domain.User{}, fmt.Errorf("%w: empty access token", ErrMALTokenExchangeFailed)
	}

	profile, err := service.oauth.FetchCurrentUser(ctx, token.AccessToken)
	if err != nil {
		return domain.User{}, fmt.Errorf("%w: %w", ErrMALCurrentUserFetchFailed, err)
	}

	malProfile, user, err := service.repo.AttachMALProfile(ctx, userID, profile)
	if err != nil {
		// Pass conflict errors (e.g. domain.ErrMALAlreadyLinked) through intact.
		return domain.User{}, err
	}

	if err := service.repo.SaveToken(ctx, malProfile.ID, *token); err != nil {
		return domain.User{}, fmt.Errorf("%w: %w", ErrAuthTokenSaveFailed, err)
	}

	return user, nil
}

// UnlinkMAL removes the MAL link for a user: the stored token is deleted and
// mal_user_id is cleared. The synced anime snapshot is intentionally kept.
func (service *AuthService) UnlinkMAL(ctx context.Context, userID int64) (domain.User, error) {
	ctx = ensureContext(ctx)
	if userID <= 0 {
		return domain.User{}, errors.New("user_id must be positive")
	}

	return service.repo.UnlinkMALProfile(ctx, userID)
}

func (service *AuthService) GetValidToken(ctx context.Context, userID int64) (*domain.MALToken, error) {
	ctx = ensureContext(ctx)

	token, found, err := service.repo.LoadToken(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load token from database: %w", err)
	}
	if !found {
		return nil, ErrNoValidToken
	}
	if token.AccessToken == "" {
		return nil, errors.New("empty access_token in database")
	}
	if !token.IsValid(time.Now()) {
		return nil, ErrTokenExpired
	}

	return &domain.MALToken{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenType:    token.TokenType,
		ExpiresIn:    token.ExpiresIn,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

func (service *AuthService) ResolveUserByUsername(ctx context.Context, username string) (domain.User, error) {
	ctx = ensureContext(ctx)

	user, found, err := service.repo.UserByUsername(ctx, username)
	if err != nil {
		return domain.User{}, err
	}
	if !found {
		return domain.User{}, ErrUserNotFound
	}

	return user, nil
}
