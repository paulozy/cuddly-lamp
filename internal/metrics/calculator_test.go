package metrics

import (
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestBuildCloneOptionsWithoutToken(t *testing.T) {
	opts := buildCloneOptions("https://github.com/owner/repo", "", "")

	if opts.URL != "https://github.com/owner/repo" {
		t.Fatalf("URL = %q, want repository URL", opts.URL)
	}
	if opts.Depth != 1 {
		t.Fatalf("Depth = %d, want 1", opts.Depth)
	}
	if opts.Auth != nil {
		t.Fatal("Auth should be nil when github token is empty")
	}
	if opts.SingleBranch {
		t.Fatal("SingleBranch should be false when branch is empty")
	}
}

func TestBuildCloneOptionsWithToken(t *testing.T) {
	opts := buildCloneOptions("https://github.com/owner/repo", "token-123", "")

	auth, ok := opts.Auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("Auth type = %T, want *http.BasicAuth", opts.Auth)
	}
	if auth.Username != "x-access-token" {
		t.Fatalf("Username = %q, want x-access-token", auth.Username)
	}
	if auth.Password != "token-123" {
		t.Fatal("Password should be set to the github token")
	}
}

func TestBuildCloneOptionsWithBranch(t *testing.T) {
	opts := buildCloneOptions("https://github.com/owner/repo", "", "develop")

	if !opts.SingleBranch {
		t.Fatal("SingleBranch should be true when branch is set")
	}
	if got := opts.ReferenceName.String(); got != "refs/heads/develop" {
		t.Fatalf("ReferenceName = %q, want refs/heads/develop", got)
	}
}
