package scm

import "context"

// The interfaces below are split by capability rather than by provider,
// because that is how the call sites actually use them: the sync service only
// reads catalog data, the pull request handler only reads change requests, and
// the docs worker only writes branches, files and change requests. A consumer
// that declares the narrow interface it needs cannot accidentally reach for
// the rest, and a test double for it stays small.

// CatalogReader supplies everything the repository catalog and the scorecard
// need about a repository.
type CatalogReader interface {
	GetRepository(ctx context.Context, ref RepoRef) (*RepoInfo, error)
	ListBranches(ctx context.Context, ref RepoRef) ([]Branch, error)
	ListCommits(ctx context.Context, ref RepoRef, branch string, limit int) ([]Commit, error)
	// ListLanguages returns the full language breakdown, not just the dominant
	// language. Values are provider-defined weights (byte counts on GitHub,
	// normalized percentages on GitLab) and are only meaningful relative to
	// each other; consumers that care about presence should use the keys.
	ListLanguages(ctx context.Context, ref RepoRef) (map[string]int, error)
	// GetTree lists the repository recursively at gitRef, which may be a
	// branch name. Check RepoTree.Truncated before concluding a path is absent.
	GetTree(ctx context.Context, ref RepoRef, gitRef string) (*RepoTree, error)
}

// ChangeRequestReader serves read-only pull/merge request browsing.
type ChangeRequestReader interface {
	// ListChangeRequests returns the open change requests for a repository.
	ListChangeRequests(ctx context.Context, ref RepoRef) ([]ChangeRequest, error)
	// GetChangeRequest looks a change request up by its user-facing number
	// (`number` on GitHub, `iid` on GitLab) — never by internal ID.
	GetChangeRequest(ctx context.Context, ref RepoRef, number int64) (*ChangeRequest, error)
	GetChangeRequestFiles(ctx context.Context, ref RepoRef, number int64) ([]ChangeRequestFile, error)
}

// ChangeRequestWriter covers the write path documentation generation needs:
// branch off, commit files, open a change request.
type ChangeRequestWriter interface {
	CreateBranch(ctx context.Context, ref RepoRef, baseBranch, newBranch string) error
	// UpsertFile creates path on branch, or updates it when it already exists.
	UpsertFile(ctx context.Context, ref RepoRef, branch, path, message, content string) error
	OpenChangeRequest(ctx context.Context, ref RepoRef, title, head, base, body string) (*ChangeRequest, error)
}

// Webhook is a registration receipt. Events records what the provider was
// actually subscribed to, whose names differ per provider (`pull_request` on
// GitHub, `merge_request` on GitLab), so the stored config reflects reality.
type Webhook struct {
	ID     string
	Events []string
}

// WebhookRegistrar registers this deployment's receiver on the repository.
type WebhookRegistrar interface {
	RegisterWebhook(ctx context.Context, ref RepoRef, webhookURL, secret string) (*Webhook, error)
}

// CloneAuthorizer supplies HTTP basic-auth credentials for `git clone` over
// HTTPS. The username is provider-specific magic (`x-access-token` on GitHub,
// `oauth2` on GitLab), which is exactly the kind of detail callers should not
// have to know.
type CloneAuthorizer interface {
	CloneAuth() (username, password string)
}

// IssueReader serves read-only issue browsing.
type IssueReader interface {
	// ListIssues returns the open issues for a repository.
	//
	// Implementations must exclude change requests. GitHub's issues endpoint
	// returns pull requests alongside real issues, so an adapter that forwards
	// the response verbatim will double-list every open PR.
	ListIssues(ctx context.Context, ref RepoRef) ([]Issue, error)
}

// ContributorReader serves the list of people credited with commits.
type ContributorReader interface {
	// ListContributors returns contributors ordered by commit count, most
	// prolific first. See Contributor for why the identity fields are provider-
	// dependent.
	ListContributors(ctx context.Context, ref RepoRef) ([]Contributor, error)
}

// IssueWriter covers the issue mutations the platform performs on the user's
// behalf. These act as the *organization's* token, not the signed-in person —
// see the handler for how attribution is preserved in the payload.
type IssueWriter interface {
	CloseIssue(ctx context.Context, ref RepoRef, number int64) error
}

// ChangeRequestReviewer submits a review verdict on a change request.
//
// RequestChanges may return ErrUnsupportedCapability: GitLab has approve and
// unapprove over REST, but no portable equivalent of GitHub's REQUEST_CHANGES
// review event. Callers must handle that rather than assume the action lands.
type ChangeRequestReviewer interface {
	ApproveChangeRequest(ctx context.Context, ref RepoRef, number int64, body string) error
	RequestChanges(ctx context.Context, ref RepoRef, number int64, body string) error
}

// Provider is a source-code host that supports every capability the platform
// uses. Depend on the narrowest interface that fits; this composition exists
// for the resolver's return type.
type Provider interface {
	CatalogReader
	ChangeRequestReader
	ChangeRequestWriter
	ChangeRequestReviewer
	IssueReader
	IssueWriter
	ContributorReader
	WebhookRegistrar
	CloneAuthorizer
}
