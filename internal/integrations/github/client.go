package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	ErrUnauthorized = errors.New("github: unauthorized — check your token")
	ErrRateLimited  = errors.New("github: API rate limit exceeded")
	ErrNotFound     = errors.New("github: resource not found")
)

// ClientInterface allows swapping the real client with a mock in tests.
type ClientInterface interface {
	GetRepository(ctx context.Context, owner, repo string) (*RepoInfo, error)
	GetBranches(ctx context.Context, owner, repo string) ([]Branch, error)
	GetCommits(ctx context.Context, owner, repo, branch string, limit int) ([]Commit, error)
	ListPullRequests(ctx context.Context, owner, repo string) ([]PullRequest, error)
	ListIssues(ctx context.Context, owner, repo string) ([]Issue, error)
	ListContributors(ctx context.Context, owner, repo string) ([]Contributor, error)
	CloseIssue(ctx context.Context, owner, repo string, number int64) error
	SubmitReview(ctx context.Context, owner, repo string, number int64, event, reviewBody string) error
	CreateWebhook(ctx context.Context, owner, repo, webhookURL, secret string) (int64, error)
	DeleteWebhook(ctx context.Context, owner, repo string, webhookID int64) error
	GetPullRequest(ctx context.Context, owner, repo string, prID int64) (*PullRequest, error)
	GetPullRequestFiles(ctx context.Context, owner, repo string, prID int64) ([]PRFile, error)
	GetRepositoryTree(ctx context.Context, owner, repo, ref string) (*RepoTree, error)
	GetLanguages(ctx context.Context, owner, repo string) (map[string]int, error)
	CreateBranch(ctx context.Context, owner, repo, baseBranch, newBranch string) error
	CreateOrUpdateFile(ctx context.Context, owner, repo, branch, path, message, content string) error
	CreatePullRequest(ctx context.Context, owner, repo, title, head, base, body string) (*PullRequest, error)
	GetAuthenticatedUser(ctx context.Context) (*AuthenticatedUser, error)
	ListReviews(ctx context.Context, owner, repo string, number int64) ([]Review, error)
}

type RepoInfo struct {
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	FullName        string   `json:"full_name"`
	Description     string   `json:"description"`
	DefaultBranch   string   `json:"default_branch"`
	Language        string   `json:"language"`
	Topics          []string `json:"topics"`
	StargazersCount int      `json:"stargazers_count"`
	ForksCount      int      `json:"forks_count"`
	OpenIssuesCount int      `json:"open_issues_count"`
	Private         bool     `json:"private"`
}

type Branch struct {
	Name string `json:"name,omitempty"`
	Ref  string `json:"ref,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

func (b Branch) DisplayName() string {
	if b.Ref != "" {
		return b.Ref
	}
	return b.Name
}

type Commit struct {
	SHA    string     `json:"sha"`
	Commit commitInfo `json:"commit"`
}

type commitInfo struct {
	Message string     `json:"message"`
	Author  commitUser `json:"author"`
}

type commitUser struct {
	Name string    `json:"name"`
	Date time.Time `json:"date"`
}

type PullRequest struct {
	ID        int64  `json:"id"`
	Number    int64  `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"` // open, closed
	User      User   `json:"user"`
	Head      Branch `json:"head"`
	Base      Branch `json:"base"`
	MergedAt  string `json:"merged_at,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Draft     bool   `json:"draft"`
	// Pointers because the *list* endpoint omits these four entirely — only
	// `GET /pulls/{n}` carries them. Decoding into plain ints turned "GitHub
	// did not say" into a confident "0 files, +0 −0".
	CommitsCount   *int   `json:"commits"`
	ChangedFiles   *int   `json:"changed_files"`
	AdditionsCount *int   `json:"additions"`
	DeletionsCount *int   `json:"deletions"`
	HTMLURL        string `json:"html_url"`
}

type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
}

// Label is an issue label. Only the name is consumed; GitHub also sends a
// color and description that nothing here renders.
type Label struct {
	Name string `json:"name"`
}

// Issue is GitHub's issue payload.
//
// PullRequest is the field that makes `GET /issues` usable: GitHub returns
// pull requests from that endpoint too, and this sub-object is present only on
// them. It is decoded as a raw pointer because its contents are irrelevant —
// presence alone is the signal. See Issue.IsPullRequest.
type Issue struct {
	ID        int64   `json:"id"`
	Number    int64   `json:"number"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	State     string  `json:"state"` // open, closed
	User      User    `json:"user"`
	Labels    []Label `json:"labels"`
	Comments  int     `json:"comments"`
	HTMLURL   string  `json:"html_url"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`

	PullRequest *struct{} `json:"pull_request,omitempty"`
}

// IsPullRequest reports whether this "issue" is really a pull request.
func (i Issue) IsPullRequest() bool { return i.PullRequest != nil }

// LabelNames flattens the label objects to their names.
func (i Issue) LabelNames() []string {
	names := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		if l.Name != "" {
			names = append(names, l.Name)
		}
	}
	return names
}

// Contributor is GitHub's contributor payload. Note there is no display name
// and no activity timestamp — `contributions` is the commit count and that is
// all this endpoint gives.
type Contributor struct {
	ID            int64  `json:"id"`
	Login         string `json:"login"`
	AvatarURL     string `json:"avatar_url"`
	Contributions int    `json:"contributions"`
	Type          string `json:"type"`
}

type Client struct {
	token      string
	httpClient *http.Client
	baseURL    string
}

func NewClient(token string) *Client {
	return &Client{
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://api.github.com",
	}
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		// 403 is two different answers on GitHub: throttling, and "your token
		// may not do that". Only the former is worth waiting out, so the
		// headers decide — see isRateLimited.
		if isRateLimited(resp.Header) {
			return ErrRateLimited
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return newAPIError(resp.StatusCode, raw)
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusNoContent:
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return newAPIError(resp.StatusCode, raw)
	}

	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*RepoInfo, error) {
	var info RepoInfo
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s", owner, repo), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) GetBranches(ctx context.Context, owner, repo string) ([]Branch, error) {
	var branches []Branch
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/branches?per_page=100", owner, repo), nil, &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

func (c *Client) GetCommits(ctx context.Context, owner, repo, branch string, limit int) ([]Commit, error) {
	var commits []Commit
	path := fmt.Sprintf("/repos/%s/%s/commits?sha=%s&per_page=%d", owner, repo, branch, limit)
	if err := c.do(ctx, http.MethodGet, path, nil, &commits); err != nil {
		return nil, err
	}
	return commits, nil
}

func (c *Client) ListPullRequests(ctx context.Context, owner, repo string) ([]PullRequest, error) {
	var prs []PullRequest
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=100", owner, repo), nil, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// ListIssues returns the open issues for a repository, with pull requests
// filtered out — see Issue.PullRequest for why they are in the response at all.
func (c *Client) ListIssues(ctx context.Context, owner, repo string) ([]Issue, error) {
	var raw []Issue
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/issues?state=open&per_page=100", owner, repo), nil, &raw); err != nil {
		return nil, err
	}
	issues := make([]Issue, 0, len(raw))
	for _, issue := range raw {
		if issue.IsPullRequest() {
			continue
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// CloseIssue closes an issue. GitHub uses the same endpoint for issues and
// pull requests, but the caller has already established this is an issue.
func (c *Client) CloseIssue(ctx context.Context, owner, repo string, number int64) error {
	body, err := json.Marshal(map[string]string{"state": "closed"})
	if err != nil {
		return fmt.Errorf("marshal close issue: %w", err)
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number)
	return c.do(ctx, http.MethodPatch, path, bytes.NewReader(body), nil)
}

// SubmitReview posts a review verdict on a pull request. Event is GitHub's
// review event vocabulary: APPROVE or REQUEST_CHANGES.
func (c *Client) SubmitReview(ctx context.Context, owner, repo string, number int64, event, reviewBody string) error {
	payload, err := json.Marshal(map[string]string{"event": event, "body": reviewBody})
	if err != nil {
		return fmt.Errorf("marshal review: %w", err)
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(payload), nil)
}

// ListContributors returns contributors to the default branch, most commits
// first (GitHub already sorts them that way).
func (c *Client) ListContributors(ctx context.Context, owner, repo string) ([]Contributor, error) {
	var contributors []Contributor
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/contributors?per_page=100", owner, repo), nil, &contributors); err != nil {
		return nil, err
	}
	return contributors, nil
}

// AuthenticatedUser is the account a token acts as.
type AuthenticatedUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Type  string `json:"type"`
}

// GetAuthenticatedUser resolves who the configured token belongs to.
//
// It answers "whose approval would this have been?" — the question a 422 about
// approving your own pull request raises and does not answer, because the
// acting identity is the organization's token rather than the person clicking.
// https://docs.github.com/en/rest/users/users#get-the-authenticated-user
func (c *Client) GetAuthenticatedUser(ctx context.Context) (*AuthenticatedUser, error) {
	var user AuthenticatedUser
	if err := c.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// Review is one recorded verdict on a pull request.
//
// State is GitHub's vocabulary: APPROVED, CHANGES_REQUESTED, COMMENTED,
// DISMISSED, PENDING. A person can review more than once, so the list is a
// history, not a set of current positions — see the adapter for how it is
// reduced.
type Review struct {
	ID          int64  `json:"id"`
	User        User   `json:"user"`
	State       string `json:"state"`
	Body        string `json:"body"`
	SubmittedAt string `json:"submitted_at"`
}

// ListReviews returns the reviews on a pull request, oldest first — the order
// GitHub returns them in, and the order the adapter depends on to find each
// reviewer's most recent position.
// https://docs.github.com/en/rest/pulls/reviews#list-reviews-for-a-pull-request
func (c *Client) ListReviews(ctx context.Context, owner, repo string, number int64) ([]Review, error) {
	var reviews []Review
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=100", owner, repo, number)
	if err := c.do(ctx, http.MethodGet, path, nil, &reviews); err != nil {
		return nil, err
	}
	return reviews, nil
}
