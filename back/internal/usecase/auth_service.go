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
	ErrMALTokenExchangeFailed    = errors.New("failed to exchange MAL authorization code")
	ErrMALCurrentUserFetchFailed = errors.New("failed to fetch MAL current user")
	ErrAuthUserSaveFailed        = errors.New("failed to save user")
	ErrAuthTokenSaveFailed       = errors.New("failed to save token")
)

type AuthService struct {
	repo        ports.AuthRepository
	oauth       ports.MALOAuthClient
	oauthConfig ports.MALOAuthConfig
}

type AuthServiceDependencies struct {
	Repo        ports.AuthRepository
	OAuth       ports.MALOAuthClient
	OAuthConfig ports.MALOAuthConfig
}

func NewAuthService(deps AuthServiceDependencies) *AuthService {
	return &AuthService{
		repo:        deps.Repo,
		oauth:       deps.OAuth,
		oauthConfig: deps.OAuthConfig,
	}
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

func (service *AuthService) CompleteMALLogin(ctx context.Context, code, verifier string) (domain.User, error) {
	ctx = ensureContext(ctx)

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

	user, err := service.repo.UpsertMALUser(ctx, profile)
	if err != nil {
		return domain.User{}, fmt.Errorf("%w: %w", ErrAuthUserSaveFailed, err)
	}

	if err := service.repo.SaveToken(ctx, user.ID, *token); err != nil {
		return domain.User{}, fmt.Errorf("%w: %w", ErrAuthTokenSaveFailed, err)
	}

	return user, nil
}

func (service *AuthService) UpsertPublicUser(ctx context.Context, username string) (domain.User, error) {
	ctx = ensureContext(ctx)

	user, err := service.repo.UpsertPublicUser(ctx, username)
	if err != nil {
		return domain.User{}, err
	}

	return user, nil
}

func (service *AuthService) ResolvePublicUser(ctx context.Context, username string) (domain.User, error) {
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
