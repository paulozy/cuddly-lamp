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

// MaxFileBytes caps a single GetFileContent read.
//
// A 300 KB package.json is generated, not written, and is the source of truth
// for nothing. The cap keeps a committed bundle from being pulled in whole.
// Callers that have TreeEntry.Size should skip oversized entries before
// asking; providers that do not report a size (GitLab's tree listing does not)
// leave this as the only guard, so implementations truncate at it rather than
// failing.
const MaxFileBytes = 256 * 1024

// FileReader reads a single file's contents at a git ref.
//
// The tree listing (CatalogReader.GetTree) says which paths exist, so callers
// fetch a known shortlist rather than probing blindly. Absence is reported as
// ErrNotFound, which callers must treat as "not there" only when the tree that
// produced the path list was complete.
//
// The three failure classes are distinct on purpose, because derivers react
// differently to each: ErrNotFound means the file is absent and the fact stays
// complete; ErrUnauthorized and any transport or rate-limit error mean the fact
// is incomplete. Collapsing them is what lets a 429 look like "the dependency
// was removed" and sweep a good edge away.
type FileReader interface {
	GetFileContent(ctx context.Context, ref RepoRef, gitRef, path string) ([]byte, error)
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

// IdentityReader reports who the credential in use belongs to.
//
// It exists to answer one question the platform could not answer before: when a
// host refuses a write because the acting identity is the resource's author,
// *which* identity was that? Writes run as the organization's configured token,
// so the answer is rarely the person who clicked, and without it the refusal
// reads as nonsense.
//
// Callers should treat this as diagnostic. It costs a request, so the value of
// knowing has to justify it — see how the review handler uses it only after a
// rejection has already happened.
type IdentityReader interface {
	CurrentUser(ctx context.Context) (*Identity, error)
}

// ChangeRequestReviewReader reports the verdicts already recorded on a change
// request.
//
// Without it the platform could submit a review and then show no trace of it —
// the approval landed on the host, and the screen that triggered it looked
// exactly as it had before. A toast is not a state.
type ChangeRequestReviewReader interface {
	// GetChangeRequestReviews returns the current verdicts. A change request
	// nobody has reviewed yields a ReviewState with an empty Decision, not an
	// error and not nil.
	GetChangeRequestReviews(ctx context.Context, ref RepoRef, number int64) (*ReviewState, error)
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
	FileReader
	ChangeRequestReader
	ChangeRequestWriter
	ChangeRequestReviewer
	ChangeRequestReviewReader
	IdentityReader
	IssueReader
	IssueWriter
	ContributorReader
	WebhookRegistrar
	CloneAuthorizer
}
