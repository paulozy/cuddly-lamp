package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

type GitLabProvider struct {
	config *oauth2.Config
}

type gitlabUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Name     string `json:"name"`
}

func NewGitLabProvider(clientID, clientSecret, redirectURL string) *GitLabProvider {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"read_user", "read_api"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://gitlab.com/oauth/authorize",
			TokenURL: "https://gitlab.com/oauth/token",
		},
	}
	return &GitLabProvider{config: cfg}
}

func (gp *GitLabProvider) Name() string {
	return "gitlab"
}

func (gp *GitLabProvider) GetAuthURL(state string) string {
	return gp.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (gp *GitLabProvider) ExchangeCode(ctx context.Context, code string) (*OAuthUserInfo, error) {
	token, err := gp.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))
	resp, err := client.Get("https://gitlab.com/api/v4/user")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitlab api error: %s", string(body))
	}

	var user gitlabUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user: %w", err)
	}

	email := user.Email
	if email == "" {
		email = fmt.Sprintf("gitlab_%d@noreply.gitlab.com", user.ID)
	}

	return &OAuthUserInfo{
		ProviderUserID: fmt.Sprintf("%d", user.ID),
		Email:          email,
		Name:           user.Name,
	}, nil
}
