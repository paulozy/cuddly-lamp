package oauth

import "context"

type OAuthUserInfo struct {
	ProviderUserID string
	Email          string
	Name           string
}

type OAuthProvider interface {
	Name() string
	GetAuthURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*OAuthUserInfo, error)
}
