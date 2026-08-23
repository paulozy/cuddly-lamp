// Package scm holds the provider-neutral view of a source-code host: the
// types the platform actually consumes, the errors callers switch on, and the
// capability interfaces a provider implements.
//
// Nothing here is GitHub- or GitLab-specific. The provider clients live in
// sibling packages (`integrations/github`, `integrations/gitlab`) and stay
// dumb HTTP clients speaking their own wire format; the adapters in this
// package translate them into these types.
//
// "Change request" is the neutral name for what GitHub calls a pull request
// and GitLab calls a merge request. The term is internal to this package —
// the public API route and its DTOs keep saying "pull request", because
// renaming them would break the frontend contract for no user-visible gain.
package scm

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Canonical errors. Every adapter maps its provider's equivalent onto these,
// so callers (notably the HTTP handlers translating to status codes) never
// need to know which forge produced the failure.
var (
	ErrNotFound     = errors.New("scm: resource not found")
	ErrUnauthorized = errors.New("scm: unauthorized — check your token")
	ErrRateLimited  = errors.New("scm: API rate limit exceeded")
	// ErrUnsupportedProvider is returned by For when no client exists for a
	// repository's host.
	ErrUnsupportedProvider = errors.New("scm: unsupported provider")
	// ErrMissingCredentials is returned by For when the provider exists but
	// the organization has no token for it.
	ErrMissingCredentials = errors.New("scm: no credentials configured for provider")
	// ErrUnsupportedCapability is returned when a provider genuinely has no
	// equivalent for an action, as opposed to failing to perform it.
	//
	// It exists so callers can tell "this host cannot do that" from "that did
	// not work", and hide the affordance instead of offering a button that is
	// guaranteed to fail. Requesting changes on a merge request is the case it
	// was added for: GitLab exposes approve and unapprove over REST, but the
	// reviewer "requested changes" state has no stable REST equivalent across
	// the versions a self-hosted instance may be running.
	ErrUnsupportedCapability = errors.New("scm: provider does not support this action")
	// ErrProviderRejected marks a failure the host understood and refused on
	// its own rules — not an outage.
	//
	// The distinction is the whole point: a rejection is permanent, so
	// reporting it as unavailability tells the caller to retry something that
	// can never succeed. Approving a change request you authored is the case
	// it was added for.
	ErrProviderRejected = errors.New("scm: provider refused this action")
	// ErrSelfReview is the one rejection specific enough to name.
	//
	// It gets its own sentinel because it is the only one where the platform
	// can say something more useful than the host did: the acting identity is
	// the change request's author, which on this platform usually means the
	// organization's token belongs to the person who opened it.
	ErrSelfReview = errors.New("scm: the acting identity authored this change request")
)

// Rejection reasons carried by ProviderError. Empty means "refused, no
// recognized reason" — still a rejection, still permanent.
const (
	ReasonSelfReview      = "self_review"
	ReasonAlreadyReviewed = "already_reviewed"
)

// ProviderError is a rejection with the upstream status kept intact.
//
// Callers switch on ErrProviderRejected or ErrSelfReview via errors.Is and
// never read Status directly; it is here because dropping it was the original
// bug, and because a log line without it cannot be diagnosed.
type ProviderError struct {
	// Provider is "github" or "gitlab".
	Provider string
	// Status is the host's HTTP status.
	Status int
	// Reason is one of the Reason* constants, or empty when the refusal was
	// not recognized. Classification is best-effort by design — see the
	// adapters — so an unrecognized refusal is still a ProviderError.
	Reason string
	// Message is the host's own words, already truncated by the client. It is
	// safe to show a user: it says what the host objected to.
	Message string
	// TokenOwner is the login the acting credential belongs to, filled in by
	// the caller after the fact when it is worth naming.
	//
	// It is not set by the adapters: discovering it costs an extra request to
	// the host, and paying that on every call to explain a failure that is rare
	// would be backwards. The handler resolves it only once a rejection has
	// already happened.
	TokenOwner string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s refused this action (%d): %s", e.Provider, e.Status, e.Message)
}

// Is makes every ProviderError an ErrProviderRejected, and additionally an
// ErrSelfReview when that is what it turned out to be. Handlers can therefore
// treat the general case without enumerating reasons, and special-case the one
// reason worth its own message.
func (e *ProviderError) Is(target error) bool {
	switch target {
	case ErrProviderRejected:
		return true
	case ErrSelfReview:
		return e.Reason == ReasonSelfReview
	default:
		return false
	}
}

// Identity is whoever a credential acts as on its host.
//
// Login is the only field callers rely on, because it is the only one both
// hosts agree on and the only one that can be compared against a change
// request's author.
type Identity struct {
	Login string
	Name  string
	// IsBot is GitHub's `type: "Bot"`, and is always false on GitLab, which has
	// no portable equivalent. Do not branch on it for anything that must behave
	// the same on both hosts.
	IsBot bool
}

// Review verdict states, as the platform reports them.
const (
	// ReviewDecisionApproved means at least one reviewer approved and nobody is
	// currently asking for changes.
	ReviewDecisionApproved = "approved"
	// ReviewDecisionChangesRequested means at least one reviewer wants changes.
	// It outranks an approval: an outstanding objection is the more important
	// fact for whoever is deciding whether to merge.
	ReviewDecisionChangesRequested = "changes_requested"
	// ReviewDecisionCommented means the change request has been reviewed, but
	// with no verdict either way.
	ReviewDecisionCommented = "commented"
)

// ReviewState is who has weighed in on a change request, and how.
//
// Decision is empty when nobody has reviewed. That is distinct from a provider
// that could not be asked — the caller represents "unknown" by holding no
// ReviewState at all, never by an empty one.
type ReviewState struct {
	Decision           string
	ApprovedBy         []string
	ChangesRequestedBy []string
}

// RepoRef identifies a repository on its host.
//
// Namespace is a single owner on GitHub, but on GitLab it can be a nested
// group path (`group/subgroup`), which is why this is not an owner/name pair
// of plain strings.
type RepoRef struct {
	Namespace string
	Name      string
}

// FullPath is the provider-side project path, e.g. `owner/repo` or
// `group/subgroup/project`.
func (r RepoRef) FullPath() string {
	if r.Namespace == "" {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// ParseRepoRef splits a `namespace/name` path, keeping every leading segment
// in the namespace so nested GitLab groups survive intact.
func ParseRepoRef(path string) (RepoRef, error) {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	idx := strings.LastIndex(trimmed, "/")
	if idx <= 0 || idx == len(trimmed)-1 {
		return RepoRef{}, fmt.Errorf("scm: %q must contain a namespace and a repository name", path)
	}
	return RepoRef{Namespace: trimmed[:idx], Name: trimmed[idx+1:]}, nil
}

// RepoInfo is the catalog-level description of a repository.
type RepoInfo struct {
	// ID is the provider's numeric identifier, stored as metadata.provider_id.
	ID   int
	Name string
	// FullName is the provider path (`owner/repo`).
	FullName      string
	Description   string
	DefaultBranch string
	// Language is the single dominant language, when the provider reports one.
	// It is only a fallback for callers — Languages() is the real breakdown.
	Language       string
	Topics         []string
	StarCount      int
	ForkCount      int
	OpenIssueCount int
	Private        bool
}

// Branch is a named ref with the commit it points at.
type Branch struct {
	Name string
	SHA  string
}

// Commit is flattened deliberately: GitHub nests the message under
// `commit.commit.message` and GitLab does not, and no caller wants to care.
type Commit struct {
	SHA        string
	Message    string
	AuthorName string
	Date       time.Time
}

// Change request states. The vocabulary is GitHub's, because the frontend
// already speaks it: a merged change request reports `closed` with MergedAt
// set, which is how GitHub has always reported it. GitLab's extra states
// (`opened`, `locked`, `merged`) are mapped onto these two by its adapter.
const (
	ChangeRequestStateOpen   = "open"
	ChangeRequestStateClosed = "closed"
)

// ChangeRequest is a pull request (GitHub) or merge request (GitLab).
//
// Timestamps stay strings because they are passed straight through to the API
// response DTOs, which are documented as provider ISO-8601 strings. Parsing
// and reformatting them here would only risk changing what clients already
// receive.
type ChangeRequest struct {
	ID     int64
	Number int64
	Title  string
	Body   string
	State  string
	// AuthorLogin is the provider username (`login` on GitHub, `username` on
	// GitLab).
	AuthorLogin string
	HeadRef     string
	HeadSHA     string
	BaseRef     string
	BaseSHA     string
	Draft       bool
	// CommitsCount, ChangedFiles, Additions and Deletions are best-effort:
	// providers expose them inconsistently. nil means the provider did not
	// report the number — which is not the same as reporting zero, and must not
	// render as "0 files changed". GitHub omits all four when listing pull
	// requests and fills them only on the detail call; GitLab reports none of
	// them on the merge request itself, so its detail view sums them from the
	// diffs.
	CommitsCount *int
	ChangedFiles *int
	Additions    *int
	Deletions    *int
	WebURL       string
	CreatedAt    string
	UpdatedAt    string
	MergedAt     string
}

// File statuses, normalized across providers.
const (
	FileStatusAdded    = "added"
	FileStatusModified = "modified"
	FileStatusRemoved  = "removed"
	FileStatusRenamed  = "renamed"
)

// ChangeRequestFile is one file changed by a change request, with its unified
// diff when the provider supplies one.
type ChangeRequestFile struct {
	SHA       string
	Path      string
	Status    string
	Additions int
	Deletions int
	Changes   int
	Patch     string
}

// Issue states, normalized across providers. GitLab says `opened`/`closed`;
// its adapter maps the first onto `open`, matching GitHub and the vocabulary
// the frontend already speaks for change requests.
const (
	IssueStateOpen   = "open"
	IssueStateClosed = "closed"
)

// Issue is a bug or task tracked on the repository's host.
//
// It is deliberately *not* a ChangeRequest: on GitHub every pull request is
// also an issue, and conflating them is exactly the bug the adapter guards
// against — `GET /issues` returns both, and only the `pull_request` field
// tells them apart.
//
// Timestamps stay strings for the same reason they do on ChangeRequest: they
// are passed through to the API DTOs as provider ISO-8601.
type Issue struct {
	Number int64
	Title  string
	State  string
	// AuthorLogin is the provider username (`user.login` on GitHub,
	// `author.username` on GitLab).
	AuthorLogin string
	Labels      []string
	// CommentsCount is best-effort: zero means none reported.
	CommentsCount int
	WebURL        string
	CreatedAt     string
	UpdatedAt     string
}

// Contributor is one person credited with commits on a repository.
//
// Login and Name are both optional because the two providers report disjoint
// halves of the identity, and neither can be synthesized from the other:
// GitHub's contributors endpoint returns a `login` and no display name, while
// GitLab's returns a `name` and an email and no username. Callers must render
// whichever is present rather than assuming either.
//
// Note what is absent: neither provider reports a contributor's change-request
// count or last activity from this endpoint. Anything of that kind has to be
// derived from data we actually fetch — and labelled for what it really is.
type Contributor struct {
	Login     string
	Name      string
	Email     string
	AvatarURL string
	// Commits is the provider's contribution count for the default branch.
	Commits int
}

// DisplayName returns the best identity the provider gave us, preferring the
// human name and falling back to the username.
func (c Contributor) DisplayName() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Login
}

// Tree entry types.
const (
	TreeEntryBlob = "blob"
	TreeEntryTree = "tree"
)

// TreeEntry is one node of a recursive repository listing. Size is 0 when the
// provider does not report it.
type TreeEntry struct {
	Path string
	Type string
	Size int
}

// RepoTree is a recursive listing of a repository at a ref.
//
// Truncated is load-bearing: both providers cap what they will return (GitHub
// by response size, GitLab by the page ceiling the adapter enforces), and a
// path missing from a truncated listing proves nothing. Callers must treat
// "not found in a truncated tree" as unknown, never as "does not exist".
type RepoTree struct {
	SHA       string
	Truncated bool
	Entries   []TreeEntry
}

// BlobPaths returns only file paths, dropping directory entries. Detection
// works on files: a `test/` directory holding no files proves nothing.
func (t *RepoTree) BlobPaths() []string {
	if t == nil {
		return nil
	}
	paths := make([]string, 0, len(t.Entries))
	for i := range t.Entries {
		if t.Entries[i].Type == TreeEntryBlob {
			paths = append(paths, t.Entries[i].Path)
		}
	}
	return paths
}
