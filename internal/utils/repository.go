package utils

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

// ParseRepositoryURL extracts the repository name (owner/repo) and detects
// the provider type from a hosted-git URL.
func ParseRepositoryURL(rawURL string) (name string, repoType models.RepositoryType, err error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid URL: %q", rawURL)
	}

	host := strings.ToLower(parsed.Host)
	switch {
	case host == "github.com":
		repoType = models.RepositoryTypeGitHub
	case host == "gitlab.com" || strings.HasSuffix(host, ".gitlab.com"):
		repoType = models.RepositoryTypeGitLab
	case strings.Contains(host, "gitea"):
		repoType = models.RepositoryTypeGitea
	default:
		return "", "", fmt.Errorf("unsupported git host %q: must be github.com, gitlab.com, or a Gitea instance", host)
	}

	// Trim leading slash and optional .git suffix, then expect at least
	// owner/repo.
	path := strings.Trim(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("URL must contain owner and repository name (e.g. https://github.com/owner/repo)")
	}
	for _, part := range parts {
		if part == "" {
			return "", "", fmt.Errorf("URL must contain owner and repository name (e.g. https://github.com/owner/repo)")
		}
	}

	// GitLab groups nest, so `group/subgroup/project` is one project's real
	// path — keeping only the first two segments would name a different
	// project, which may well exist and would then be synced instead.
	// GitHub and Gitea have no nesting: anything past owner/repo there is a
	// deep link (`/tree/main/...`), so it is dropped.
	if repoType == models.RepositoryTypeGitLab {
		return strings.Join(parts, "/"), repoType, nil
	}
	return parts[0] + "/" + parts[1], repoType, nil
}
