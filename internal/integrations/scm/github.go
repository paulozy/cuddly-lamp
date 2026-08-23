package scm

import (
	"context"
	"errors"
	"strconv"

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

// translateGitHubErr maps the client's sentinels onto the canonical ones,
// leaving anything else untouched so the original message survives.
func translateGitHubErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, github.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, github.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, github.ErrRateLimited):
		return ErrRateLimited
	default:
		return err
	}
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
