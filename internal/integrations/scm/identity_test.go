package scm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

// identityClient serves only the authenticated-user call; every other method
// comes from the embedded nil interface and would panic if reached.
type identityClient struct {
	github.ClientInterface
	user *github.AuthenticatedUser
	err  error
}

func (c *identityClient) GetAuthenticatedUser(_ context.Context) (*github.AuthenticatedUser, error) {
	return c.user, c.err
}

func TestGitHubProvider_CurrentUserMapsTheAccount(t *testing.T) {
	provider := NewGitHubProviderWithClient(&identityClient{
		user: &github.AuthenticatedUser{Login: "paulozy", Name: "Paulo Abreu"},
	}, "token")

	identity, err := provider.CurrentUser(context.Background())
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if identity.Login != "paulozy" || identity.Name != "Paulo Abreu" {
		t.Errorf("identity = %+v, want paulozy / Paulo Abreu", identity)
	}
	if identity.IsBot {
		t.Error("IsBot = true, want false for a User account")
	}
}

// A machine account is worth distinguishing on GitHub, which reports it.
func TestGitHubProvider_CurrentUserDetectsABotAccount(t *testing.T) {
	provider := NewGitHubProviderWithClient(&identityClient{
		user: &github.AuthenticatedUser{Login: "renovate[bot]", Type: "Bot"},
	}, "token")

	identity, err := provider.CurrentUser(context.Background())
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if !identity.IsBot {
		t.Error("IsBot = false, want true when GitHub reports type Bot")
	}
}

func TestGitHubProvider_CurrentUserTranslatesErrors(t *testing.T) {
	provider := NewGitHubProviderWithClient(&identityClient{err: github.ErrUnauthorized}, "token")

	if _, err := provider.CurrentUser(context.Background()); err != ErrUnauthorized {
		t.Errorf("err = %v, want the canonical ErrUnauthorized", err)
	}
}

// GitLab has no account-type field, so IsBot must stay false rather than being
// guessed from the username.
func TestGitLabProvider_CurrentUserMapsUsernameAndNeverGuessesBot(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewEncoder(w).Encode(map[string]string{"username": "project_bot", "name": "Project Bot"})
	}))
	defer srv.Close()

	identity, err := newGitLabProvider(srv).CurrentUser(context.Background())
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if identity.Login != "project_bot" || identity.Name != "Project Bot" {
		t.Errorf("identity = %+v, want project_bot / Project Bot", identity)
	}
	if identity.IsBot {
		t.Error("IsBot = true, but GitLab reports no account type — it must not be inferred")
	}
	if gotPath != "/user" {
		t.Errorf("path = %q, want /user", gotPath)
	}
}
