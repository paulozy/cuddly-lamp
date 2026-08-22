package scm

import (
	"fmt"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

// Credentials carries one token per provider. A repository is synced with the
// token of its own host, never with whatever token happens to be configured —
// that is the whole point of resolving per repository.
type Credentials struct {
	GitHubToken string
	GitLabToken string
	// GitLabBaseURL points the client at a GitLab other than gitlab.com. Empty
	// means gitlab.com.
	GitLabBaseURL string
}

// CredentialsFromConfig reads an organization's tokens, falling back to the
// platform-level tokens from the server environment for any the organization
// has not set.
func CredentialsFromConfig(cfg *models.OrganizationConfig, fallback Credentials) Credentials {
	creds := fallback
	if cfg == nil {
		return creds
	}
	if cfg.GithubToken != "" {
		creds.GitHubToken = cfg.GithubToken
	}
	if cfg.GitlabToken != "" {
		creds.GitLabToken = cfg.GitlabToken
	}
	if cfg.GitlabBaseURL != "" {
		creds.GitLabBaseURL = cfg.GitlabBaseURL
	}
	return creds
}

// HostsOnly returns credentials carrying the deployment's provider API roots
// and no tokens.
//
// Read paths use this as their fallback. Falling back to the platform's *token*
// would let one organization browse another's code, but falling back to its
// *host* is required: the API root is a deployment fact, and without it a
// self-hosted repository would be queried against gitlab.com instead.
func HostsOnly(gitLabBaseURL string) Credentials {
	return Credentials{GitLabBaseURL: gitLabBaseURL}
}

// ResolverFunc builds the provider for a repository type. Consumers hold one
// of these instead of calling For directly, so tests can inject a stub
// provider without reaching the network.
type ResolverFunc func(kind models.RepositoryType, creds Credentials) (Provider, error)

// For returns the provider that speaks to the given repository's host.
//
// Dispatching on the repository type is what keeps `owner/repo` from being
// queried against the wrong forge: the path is not unique across hosts, so a
// GitLab project run through the GitHub client silently imports an unrelated
// project's data.
func For(kind models.RepositoryType, creds Credentials) (Provider, error) {
	switch kind {
	case models.RepositoryTypeGitHub:
		if creds.GitHubToken == "" {
			return nil, fmt.Errorf("%w: github", ErrMissingCredentials)
		}
		return NewGitHubProvider(creds.GitHubToken), nil
	case models.RepositoryTypeGitLab:
		if creds.GitLabToken == "" {
			return nil, fmt.Errorf("%w: gitlab", ErrMissingCredentials)
		}
		return NewGitLabProviderWithBaseURL(creds.GitLabToken, creds.GitLabBaseURL), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, kind)
	}
}
