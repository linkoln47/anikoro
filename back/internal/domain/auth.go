package domain

import "time"

type MALToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (token *MALToken) IsValid(now time.Time) bool {
	return token != nil && token.AccessToken != "" && now.Before(token.ExpiresAt)
}

type User struct {
	ID        int64
	MALUserID int64
	Username  string
}

type MALUserProfile struct {
	ID       int64
	Username string
}
