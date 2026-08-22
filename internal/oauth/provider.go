package oauth

import "context"

type OAuthUserInfo struct {
	ProviderUserID string
	// Username is the provider login (GitHub `login`, GitLab `username`). It is
	// what identifies the person on a change request, which the numeric
	// ProviderUserID does not — verified onboarding steps depend on it.
	Username string
	Email    string
	Name     string
}

type OAuthProvider interface {
	Name() string
	GetAuthURL(state string) string
	ExchangeCode(ctx context.Context, code string) (*OAuthUserInfo, error)
}
