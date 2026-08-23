package scm

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/gitlab"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// gitlabProvider adapts the GitLab REST v4 client to the neutral interfaces.
//
// Three GitLab shapes differ enough from GitHub's that the mapping is more
// than renaming: language percentages instead of byte counts, a paginated tree
// with no truncation flag of its own, and per-file diffs with no line counts.
// Each is handled below, and each is a place where a naive mapping would
// silently report something false.
type gitlabProvider struct {
	client *gitlab.Client
	token  string
}

// NewGitLabProvider wraps a gitlab.com client as a Provider.
func NewGitLabProvider(token string) Provider {
	return &gitlabProvider{client: gitlab.NewClient(token), token: token}
}

// NewGitLabProviderWithBaseURL points the client at a specific API root. Used
// by tests against httptest; self-hosted GitLab is not supported yet.
func NewGitLabProviderWithBaseURL(token, baseURL string) Provider {
	return &gitlabProvider{client: gitlab.NewClientWithBaseURL(token, baseURL), token: token}
}

func translateGitLabErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gitlab.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, gitlab.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, gitlab.ErrRateLimited):
		return ErrRateLimited
	default:
		return err
	}
}

// CloneAuth returns the basic-auth pair for an HTTPS clone. GitLab accepts any
// non-empty username when the password is an access token, and `oauth2` is the
// conventional choice that also works for OAuth tokens.
// https://docs.gitlab.com/user/profile/personal_access_tokens/
func (p *gitlabProvider) CloneAuth() (string, string) {
	return "oauth2", p.token
}

func (p *gitlabProvider) GetRepository(ctx context.Context, ref RepoRef) (*RepoInfo, error) {
	project, err := p.client.GetProject(ctx, ref.FullPath())
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	openIssues := 0
	if project.OpenIssuesCount != nil {
		openIssues = *project.OpenIssuesCount
	}
	return &RepoInfo{
		ID:            project.ID,
		Name:          project.Name,
		FullName:      project.PathWithNamespace,
		Description:   project.Description,
		DefaultBranch: project.DefaultBranch,
		// GitLab reports no dominant language on the project itself; the full
		// breakdown comes from ListLanguages.
		Language:       "",
		Topics:         project.Topics,
		StarCount:      project.StarCount,
		ForkCount:      project.ForksCount,
		OpenIssueCount: openIssues,
		Private:        project.Visibility != "public",
	}, nil
}

func (p *gitlabProvider) ListBranches(ctx context.Context, ref RepoRef) ([]Branch, error) {
	branches, err := p.client.ListBranches(ctx, ref.FullPath())
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make([]Branch, 0, len(branches))
	for _, b := range branches {
		out = append(out, Branch{Name: b.Name, SHA: b.Commit.ID})
	}
	return out, nil
}

func (p *gitlabProvider) ListCommits(ctx context.Context, ref RepoRef, branch string, limit int) ([]Commit, error) {
	commits, err := p.client.ListCommits(ctx, ref.FullPath(), branch, limit)
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make([]Commit, 0, len(commits))
	for _, c := range commits {
		out = append(out, Commit{
			SHA:        c.ID,
			Message:    c.Message,
			AuthorName: c.AuthorName,
			Date:       c.CommittedDate,
		})
	}
	return out, nil
}

// ListLanguages converts GitLab's percentages into the same integer-weight
// shape GitHub's byte counts produce. Only the keys carry meaning downstream
// (detection asks which languages are present), so the rounding below loses
// nothing that is used — but a language at 0.4% is rounded up to 1 rather than
// to 0, because dropping it to zero would read as "absent".
func (p *gitlabProvider) ListLanguages(ctx context.Context, ref RepoRef) (map[string]int, error) {
	languages, err := p.client.GetLanguages(ctx, ref.FullPath())
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make(map[string]int, len(languages))
	for name, percent := range languages {
		weight := int(math.Round(percent))
		if weight < 1 && percent > 0 {
			weight = 1
		}
		out[name] = weight
	}
	return out, nil
}

func (p *gitlabProvider) GetTree(ctx context.Context, ref RepoRef, gitRef string) (*RepoTree, error) {
	tree, err := p.client.GetTree(ctx, ref.FullPath(), gitRef)
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	if tree == nil {
		return nil, nil
	}
	entries := make([]TreeEntry, 0, len(tree.Entries))
	for _, e := range tree.Entries {
		entries = append(entries, TreeEntry{Path: e.Path, Type: e.Type})
	}
	// Truncated comes from the client's page ceiling, standing in for the flag
	// GitHub sends and GitLab does not.
	return &RepoTree{Truncated: tree.Truncated, Entries: entries}, nil
}

func (p *gitlabProvider) ListChangeRequests(ctx context.Context, ref RepoRef) ([]ChangeRequest, error) {
	mrs, err := p.client.ListMergeRequests(ctx, ref.FullPath())
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make([]ChangeRequest, 0, len(mrs))
	for i := range mrs {
		out = append(out, gitlabChangeRequest(&mrs[i]))
	}
	return out, nil
}

func (p *gitlabProvider) ListIssues(ctx context.Context, ref RepoRef) ([]Issue, error) {
	issues, err := p.client.ListIssues(ctx, ref.FullPath())
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make([]Issue, 0, len(issues))
	for i := range issues {
		out = append(out, Issue{
			// IID, not ID: the per-project number people actually cite, matching
			// how the change-request adapter treats merge requests.
			Number:        issues[i].IID,
			Title:         issues[i].Title,
			State:         gitlabIssueState(issues[i].State),
			AuthorLogin:   issues[i].Author.Username,
			Labels:        issues[i].Labels,
			CommentsCount: issues[i].UserNotesCount,
			WebURL:        issues[i].WebURL,
			CreatedAt:     issues[i].CreatedAt,
			UpdatedAt:     issues[i].UpdatedAt,
		})
	}
	return out, nil
}

// gitlabIssueState maps GitLab's `opened` onto the canonical `open`, leaving
// `closed` alone since both providers already agree on it.
func gitlabIssueState(state string) string {
	if state == "opened" {
		return IssueStateOpen
	}
	return IssueStateClosed
}

func (p *gitlabProvider) CloseIssue(ctx context.Context, ref RepoRef, number int64) error {
	return translateGitLabErr(p.client.CloseIssue(ctx, ref.FullPath(), number))
}

// ApproveChangeRequest approves and then records who asked for it.
//
// The approval itself carries no message on GitLab, so the note is the only
// place the acting user's name can survive — the API call is authenticated as
// the organization's token, not as them. A failed note is not worth undoing a
// successful approval, so it only warns.
func (p *gitlabProvider) ApproveChangeRequest(ctx context.Context, ref RepoRef, number int64, body string) error {
	if err := p.client.ApproveMergeRequest(ctx, ref.FullPath(), number); err != nil {
		return translateGitLabErr(err)
	}
	if body == "" {
		return nil
	}
	if err := p.client.CreateMergeRequestNote(ctx, ref.FullPath(), number, body); err != nil {
		utils.Warn("gitlab: approved the merge request but could not post the attribution note",
			"project", ref.FullPath(), "iid", number, "error", err)
	}
	return nil
}

// RequestChanges has no portable GitLab equivalent. Reporting that plainly is
// the point: the alternative — posting a note and calling it a review — would
// leave the merge request approvable by anyone who glanced at its state.
func (p *gitlabProvider) RequestChanges(_ context.Context, _ RepoRef, _ int64, _ string) error {
	return ErrUnsupportedCapability
}

func (p *gitlabProvider) ListContributors(ctx context.Context, ref RepoRef) ([]Contributor, error) {
	contributors, err := p.client.ListContributors(ctx, ref.FullPath())
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make([]Contributor, 0, len(contributors))
	for i := range contributors {
		// Login stays empty on purpose: GitLab's contributor endpoint reports
		// no username, and deriving one from the email would be a guess.
		out = append(out, Contributor{
			Name:    contributors[i].Name,
			Email:   contributors[i].Email,
			Commits: contributors[i].Commits,
		})
	}
	return out, nil
}

func (p *gitlabProvider) GetChangeRequest(ctx context.Context, ref RepoRef, number int64) (*ChangeRequest, error) {
	mr, err := p.client.GetMergeRequest(ctx, ref.FullPath(), number)
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	cr := gitlabChangeRequest(mr)

	// GitLab reports no line counts on the merge request itself, so the detail
	// view sums them from the diffs. Failing to fetch them leaves the counts
	// unreported rather than failing the request — the metadata is still useful,
	// and nil says "we do not know" instead of claiming nothing changed.
	if diffs, diffErr := p.client.ListMergeRequestDiffs(ctx, ref.FullPath(), number); diffErr == nil {
		files, added, removed := len(diffs), 0, 0
		for i := range diffs {
			additions, deletions := countDiffLines(diffs[i].Diff)
			added += additions
			removed += deletions
		}
		cr.ChangedFiles, cr.Additions, cr.Deletions = &files, &added, &removed
	}
	return &cr, nil
}

func (p *gitlabProvider) GetChangeRequestFiles(ctx context.Context, ref RepoRef, number int64) ([]ChangeRequestFile, error) {
	diffs, err := p.client.ListMergeRequestDiffs(ctx, ref.FullPath(), number)
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	out := make([]ChangeRequestFile, 0, len(diffs))
	for i := range diffs {
		diff := diffs[i]
		additions, deletions := countDiffLines(diff.Diff)
		path := diff.NewPath
		if path == "" {
			path = diff.OldPath
		}
		out = append(out, ChangeRequestFile{
			Path:      path,
			Status:    gitlabFileStatus(diff),
			Additions: additions,
			Deletions: deletions,
			Changes:   additions + deletions,
			Patch:     diff.Diff,
		})
	}
	return out, nil
}

func (p *gitlabProvider) CreateBranch(ctx context.Context, ref RepoRef, baseBranch, newBranch string) error {
	return translateGitLabErr(p.client.CreateBranch(ctx, ref.FullPath(), baseBranch, newBranch))
}

func (p *gitlabProvider) UpsertFile(ctx context.Context, ref RepoRef, branch, path, message, content string) error {
	return translateGitLabErr(p.client.UpsertFile(ctx, ref.FullPath(), branch, path, message, content))
}

func (p *gitlabProvider) OpenChangeRequest(ctx context.Context, ref RepoRef, title, head, base, body string) (*ChangeRequest, error) {
	mr, err := p.client.CreateMergeRequest(ctx, ref.FullPath(), head, base, title, body)
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	if mr == nil {
		return nil, nil
	}
	cr := gitlabChangeRequest(mr)
	return &cr, nil
}

func (p *gitlabProvider) RegisterWebhook(ctx context.Context, ref RepoRef, webhookURL, secret string) (*Webhook, error) {
	id, err := p.client.CreateHook(ctx, ref.FullPath(), webhookURL, secret)
	if err != nil {
		return nil, translateGitLabErr(err)
	}
	return &Webhook{ID: strconv.FormatInt(id, 10), Events: gitlab.HookEvents}, nil
}

func gitlabChangeRequest(mr *gitlab.MergeRequest) ChangeRequest {
	return ChangeRequest{
		ID:     mr.ID,
		Number: mr.IID, // the per-project number, which is what URLs and users use
		Title:  mr.Title,
		Body:   mr.Description,
		State:  gitlabState(mr.State),
		// GitLab's `draft` boolean carries what GitHub calls a draft PR.
		Draft:        mr.Draft,
		AuthorLogin:  mr.Author.Username,
		HeadRef:      mr.SourceBranch,
		HeadSHA:      mr.SHA,
		BaseRef:      mr.TargetBranch,
		BaseSHA:      mr.DiffRefs.BaseSHA,
		ChangedFiles: mr.ChangedFileCount(), // nil when GitLab sent no changes_count
		WebURL:       mr.WebURL,
		CreatedAt:    mr.CreatedAt,
		UpdatedAt:    mr.UpdatedAt,
		MergedAt:     mr.MergedAt,
	}
}

// gitlabState folds GitLab's four states into the two the platform speaks.
// `merged` becomes closed with MergedAt set, which is exactly how GitHub
// reports a merged pull request — so the frontend needs no new case.
func gitlabState(state string) string {
	switch state {
	case "opened", "locked":
		return ChangeRequestStateOpen
	default:
		return ChangeRequestStateClosed
	}
}

func gitlabFileStatus(diff gitlab.Diff) string {
	switch {
	case diff.NewFile:
		return FileStatusAdded
	case diff.DeletedFile:
		return FileStatusRemoved
	case diff.RenamedFile:
		return FileStatusRenamed
	default:
		return FileStatusModified
	}
}

// countDiffLines derives added and removed line counts from a unified diff,
// which is the only place GitLab exposes them.
//
// The diff body starts at the first hunk header, so there are no `+++`/`---`
// file headers to skip — but they are guarded against anyway, since counting
// them would inflate every file by one addition and one deletion.
func countDiffLines(diff string) (additions, deletions int) {
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			additions++
		case strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}
