package scm

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

// githubProvider adapts the GitHub REST client to the neutral interfaces.
//
// It holds no logic of its own: every method forwards to the client and
// translates shapes and sentinel errors. Keeping the translation here — rather
// than rewriting the client to speak neutral types — is what lets the client's
// own tests keep asserting against the real GitHub wire format.
type githubProvider struct {
	client github.ClientInterface
	token  string
}

// NewGitHubProvider wraps a GitHub client as a Provider.
func NewGitHubProvider(token string) Provider {
	return &githubProvider{client: github.NewClient(token), token: token}
}

// NewGitHubProviderWithClient wraps an existing client, for tests and for the
// platform-level client built once at startup.
func NewGitHubProviderWithClient(client github.ClientInterface, token string) Provider {
	return &githubProvider{client: client, token: token}
}

// githubEvents are the events the GitHub client subscribes a hook to. It
// mirrors the hardcoded list in github.CreateWebhook.
var githubEvents = []string{"push", "pull_request", "issues"}

// translateGitHubErr maps the client's sentinels onto the canonical ones, and
// turns a status-carrying APIError into a classified rejection.
func translateGitHubErr(err error) error {
	var apiErr *github.APIError

	switch {
	case err == nil:
		return nil
	case errors.Is(err, github.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, github.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, github.ErrRateLimited):
		return ErrRateLimited
	case errors.As(err, &apiErr):
		return classifyGitHubAPIError(apiErr)
	default:
		return err
	}
}

// classifyGitHubAPIError decides whether the host refused the action or simply
// could not answer.
//
// Anything below 500 is a refusal: GitHub read the request and said no, and no
// amount of retrying changes that. 5xx and anything unrecognized stay
// unavailability, which is the honest reading of a server that broke.
func classifyGitHubAPIError(apiErr *github.APIError) error {
	if apiErr.Status >= 500 {
		return apiErr
	}
	return &ProviderError{
		Provider: "github",
		Status:   apiErr.Status,
		Reason:   githubRejectionReason(apiErr),
		Message:  githubRejectionMessage(apiErr),
	}
}

// githubRejectionReason recognizes the refusals worth naming.
//
// Matching on prose is fragile and that is accounted for: an unrecognized
// refusal still returns a ProviderError with the host's message intact, so a
// wording change on GitHub's side costs a tailored message and nothing more.
// It must never fall back to reporting the failure as an outage.
func githubRejectionReason(apiErr *github.APIError) string {
	haystack := strings.ToLower(strings.Join(append([]string{apiErr.Message}, apiErr.Errors...), " "))
	switch {
	// Matches GitHub's "Can not approve your own pull request" and any
	// rewording that keeps the phrase.
	case strings.Contains(haystack, "approve your own"):
		return ReasonSelfReview
	case strings.Contains(haystack, "already approved"):
		return ReasonAlreadyReviewed
	default:
		return ""
	}
}

// githubRejectionMessage prefers the `errors` detail over the top-level
// message, because that is where GitHub puts the sentence a person can act on
// — "Unprocessable Entity" alone explains nothing.
func githubRejectionMessage(apiErr *github.APIError) string {
	if detail := strings.Join(apiErr.Errors, "; "); detail != "" {
		return detail
	}
	return apiErr.Message
}

func (p *githubProvider) CloneAuth() (string, string) {
	return "x-access-token", p.token
}

func (p *githubProvider) GetRepository(ctx context.Context, ref RepoRef) (*RepoInfo, error) {
	info, err := p.client.GetRepository(ctx, ref.Namespace, ref.Name)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	return &RepoInfo{
		ID:             info.ID,
		Name:           info.Name,
		FullName:       info.FullName,
		Description:    info.Description,
		DefaultBranch:  info.DefaultBranch,
		Language:       info.Language,
		Topics:         info.Topics,
		StarCount:      info.StargazersCount,
		ForkCount:      info.ForksCount,
		OpenIssueCount: info.OpenIssuesCount,
		Private:        info.Private,
	}, nil
}

func (p *githubProvider) ListBranches(ctx context.Context, ref RepoRef) ([]Branch, error) {
	branches, err := p.client.GetBranches(ctx, ref.Namespace, ref.Name)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	out := make([]Branch, 0, len(branches))
	for _, b := range branches {
		out = append(out, Branch{Name: b.DisplayName(), SHA: b.SHA})
	}
	return out, nil
}

func (p *githubProvider) ListCommits(ctx context.Context, ref RepoRef, branch string, limit int) ([]Commit, error) {
	commits, err := p.client.GetCommits(ctx, ref.Namespace, ref.Name, branch, limit)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	out := make([]Commit, 0, len(commits))
	for _, c := range commits {
		out = append(out, Commit{
			SHA:        c.SHA,
			Message:    c.Commit.Message,
			AuthorName: c.Commit.Author.Name,
			Date:       c.Commit.Author.Date,
		})
	}
	return out, nil
}

func (p *githubProvider) ListLanguages(ctx context.Context, ref RepoRef) (map[string]int, error) {
	languages, err := p.client.GetLanguages(ctx, ref.Namespace, ref.Name)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	return languages, nil
}

func (p *githubProvider) GetTree(ctx context.Context, ref RepoRef, gitRef string) (*RepoTree, error) {
	tree, err := p.client.GetRepositoryTree(ctx, ref.Namespace, ref.Name, gitRef)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	if tree == nil {
		return nil, nil
	}
	entries := make([]TreeEntry, 0, len(tree.Tree))
	for _, e := range tree.Tree {
		entries = append(entries, TreeEntry{Path: e.Path, Type: e.Type, Size: e.Size})
	}
	return &RepoTree{SHA: tree.SHA, Truncated: tree.Truncated, Entries: entries}, nil
}

func (p *githubProvider) ListChangeRequests(ctx context.Context, ref RepoRef) ([]ChangeRequest, error) {
	prs, err := p.client.ListPullRequests(ctx, ref.Namespace, ref.Name)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	out := make([]ChangeRequest, 0, len(prs))
	for _, pr := range prs {
		out = append(out, githubChangeRequest(pr))
	}
	return out, nil
}

func (p *githubProvider) ListIssues(ctx context.Context, ref RepoRef) ([]Issue, error) {
	// The client already drops pull requests from the response; see
	// github.Client.ListIssues.
	issues, err := p.client.ListIssues(ctx, ref.Namespace, ref.Name)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	out := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		out = append(out, Issue{
			Number:        issue.Number,
			Title:         issue.Title,
			State:         issue.State,
			AuthorLogin:   issue.User.Login,
			Labels:        issue.LabelNames(),
			CommentsCount: issue.Comments,
			WebURL:        issue.HTMLURL,
			CreatedAt:     issue.CreatedAt,
			UpdatedAt:     issue.UpdatedAt,
		})
	}
	return out, nil
}

func (p *githubProvider) CloseIssue(ctx context.Context, ref RepoRef, number int64) error {
	return translateGitHubErr(p.client.CloseIssue(ctx, ref.Namespace, ref.Name, number))
}

func (p *githubProvider) ApproveChangeRequest(ctx context.Context, ref RepoRef, number int64, body string) error {
	return translateGitHubErr(p.client.SubmitReview(ctx, ref.Namespace, ref.Name, number, "APPROVE", body))
}

func (p *githubProvider) RequestChanges(ctx context.Context, ref RepoRef, number int64, body string) error {
	return translateGitHubErr(p.client.SubmitReview(ctx, ref.Namespace, ref.Name, number, "REQUEST_CHANGES", body))
}

func (p *githubProvider) ListContributors(ctx context.Context, ref RepoRef) ([]Contributor, error) {
	contributors, err := p.client.ListContributors(ctx, ref.Namespace, ref.Name)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	out := make([]Contributor, 0, len(contributors))
	for _, c := range contributors {
		// Name stays empty on purpose: this endpoint returns only a login, and
		// resolving each one to a display name would cost a request per person.
		out = append(out, Contributor{
			Login:     c.Login,
			AvatarURL: c.AvatarURL,
			Commits:   c.Contributions,
		})
	}
	return out, nil
}

func (p *githubProvider) GetChangeRequest(ctx context.Context, ref RepoRef, number int64) (*ChangeRequest, error) {
	pr, err := p.client.GetPullRequest(ctx, ref.Namespace, ref.Name, number)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	if pr == nil {
		return nil, ErrNotFound
	}
	cr := githubChangeRequest(*pr)
	return &cr, nil
}

func (p *githubProvider) GetChangeRequestFiles(ctx context.Context, ref RepoRef, number int64) ([]ChangeRequestFile, error) {
	files, err := p.client.GetPullRequestFiles(ctx, ref.Namespace, ref.Name, number)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	out := make([]ChangeRequestFile, 0, len(files))
	for _, f := range files {
		out = append(out, ChangeRequestFile{
			SHA:       f.SHA,
			Path:      f.Filename,
			Status:    f.Status,
			Additions: f.Additions,
			Deletions: f.Deletions,
			Changes:   f.Changes,
			Patch:     f.Patch,
		})
	}
	return out, nil
}

func (p *githubProvider) CreateBranch(ctx context.Context, ref RepoRef, baseBranch, newBranch string) error {
	return translateGitHubErr(p.client.CreateBranch(ctx, ref.Namespace, ref.Name, baseBranch, newBranch))
}

func (p *githubProvider) UpsertFile(ctx context.Context, ref RepoRef, branch, path, message, content string) error {
	return translateGitHubErr(p.client.CreateOrUpdateFile(ctx, ref.Namespace, ref.Name, branch, path, message, content))
}

func (p *githubProvider) OpenChangeRequest(ctx context.Context, ref RepoRef, title, head, base, body string) (*ChangeRequest, error) {
	pr, err := p.client.CreatePullRequest(ctx, ref.Namespace, ref.Name, title, head, base, body)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	if pr == nil {
		return nil, nil
	}
	cr := githubChangeRequest(*pr)
	return &cr, nil
}

func (p *githubProvider) RegisterWebhook(ctx context.Context, ref RepoRef, webhookURL, secret string) (*Webhook, error) {
	id, err := p.client.CreateWebhook(ctx, ref.Namespace, ref.Name, webhookURL, secret)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	return &Webhook{ID: strconv.FormatInt(id, 10), Events: githubEvents}, nil
}

func githubChangeRequest(pr github.PullRequest) ChangeRequest {
	return ChangeRequest{
		ID:           pr.ID,
		Number:       pr.Number,
		Title:        pr.Title,
		Body:         pr.Body,
		State:        pr.State,
		AuthorLogin:  pr.User.Login,
		HeadRef:      pr.Head.DisplayName(),
		HeadSHA:      pr.Head.SHA,
		BaseRef:      pr.Base.DisplayName(),
		BaseSHA:      pr.Base.SHA,
		Draft:        pr.Draft,
		CommitsCount: pr.CommitsCount,
		ChangedFiles: pr.ChangedFiles,
		Additions:    pr.AdditionsCount,
		Deletions:    pr.DeletionsCount,
		WebURL:       pr.HTMLURL,
		CreatedAt:    pr.CreatedAt,
		UpdatedAt:    pr.UpdatedAt,
		MergedAt:     pr.MergedAt,
	}
}

func (p *githubProvider) CurrentUser(ctx context.Context) (*Identity, error) {
	user, err := p.client.GetAuthenticatedUser(ctx)
	if err != nil {
		return nil, translateGitHubErr(err)
	}
	return &Identity{
		Login: user.Login,
		Name:  user.Name,
		IsBot: user.Type == "Bot",
	}, nil
}

// GetChangeRequestReviews reduces GitHub's review history to a current verdict.
//
// Two GitHub behaviours make this more than a filter. A person can review
// repeatedly, so only their latest verdict counts — replaying the list in order
// and overwriting per login is what implements that. And COMMENTED reviews do
// not change anyone's position, so they mark the change request as reviewed
// without displacing an earlier approval or objection; DISMISSED and PENDING
// are not positions at all.
//
// Changes-requested outranks approved in the summary: an unresolved objection
// is the more important fact for someone deciding whether to merge.
func (p *githubProvider) GetChangeRequestReviews(ctx context.Context, ref RepoRef, number int64) (*ReviewState, error) {
	reviews, err := p.client.ListReviews(ctx, ref.Namespace, ref.Name, number)
	if err != nil {
		return nil, translateGitHubErr(err)
	}

	// Latest verdict per reviewer, in the order GitHub returned them.
	verdicts := make(map[string]string, len(reviews))
	commented := false
	for _, review := range reviews {
		login := review.User.Login
		if login == "" {
			continue
		}
		switch strings.ToUpper(review.State) {
		case "APPROVED":
			verdicts[login] = ReviewDecisionApproved
		case "CHANGES_REQUESTED":
			verdicts[login] = ReviewDecisionChangesRequested
		case "DISMISSED":
			// A dismissed review is explicitly no longer a position.
			delete(verdicts, login)
		case "COMMENTED":
			commented = true
		}
	}

	state := &ReviewState{}
	for login, verdict := range verdicts {
		switch verdict {
		case ReviewDecisionApproved:
			state.ApprovedBy = append(state.ApprovedBy, login)
		case ReviewDecisionChangesRequested:
			state.ChangesRequestedBy = append(state.ChangesRequestedBy, login)
		}
	}
	// Sorted so the same state does not render in a different order on every
	// request — Go randomizes map iteration.
	sort.Strings(state.ApprovedBy)
	sort.Strings(state.ChangesRequestedBy)

	switch {
	case len(state.ChangesRequestedBy) > 0:
		state.Decision = ReviewDecisionChangesRequested
	case len(state.ApprovedBy) > 0:
		state.Decision = ReviewDecisionApproved
	case commented:
		state.Decision = ReviewDecisionCommented
	}
	return state, nil
}
