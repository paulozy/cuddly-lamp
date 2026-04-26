package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GitHubProvider struct {
	config *oauth2.Config
}

type githubUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func NewGitHubProvider(clientID, clientSecret, redirectURL string) *GitHubProvider {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"read:user", "user:email"},
		Endpoint:     github.Endpoint,
	}
	return &GitHubProvider{config: cfg}
}

func (gp *GitHubProvider) Name() string {
	return "github"
}

func (gp *GitHubProvider) GetAuthURL(state string) string {
	return gp.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (gp *GitHubProvider) ExchangeCode(ctx context.Context, code string) (*OAuthUserInfo, error) {
	token, err := gp.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %s", string(body))
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user: %w", err)
	}

	email := user.Email
	if email == "" {
		email = fmt.Sprintf("github_%d@noreply.github.com", user.ID)
	}

	return &OAuthUserInfo{
		ProviderUserID: fmt.Sprintf("%d", user.ID),
		Email:          email,
		Name:           user.Name,
	}, nil
}
