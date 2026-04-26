package app

import (
	"test/internal/ports"
	"test/internal/usecase"
)

type AuthService = usecase.AuthService

func newAuthService(config *AppConfig, repo ports.AuthRepository, oauth ports.MALOAuthClient) *AuthService {
	return usecase.NewAuthService(usecase.AuthServiceDependencies{
		Repo:  repo,
		OAuth: oauth,
		OAuthConfig: ports.MALOAuthConfig{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURI:  config.RedirectURI,
		},
	})
}

func (a *App) authService() *AuthService {
	return a.Auth
}
