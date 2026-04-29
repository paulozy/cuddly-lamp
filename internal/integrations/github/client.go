package github

import (
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
	CreateWebhook(ctx context.Context, owner, repo, webhookURL, secret string) (int64, error)
	DeleteWebhook(ctx context.Context, owner, repo string, webhookID int64) error
	GetPullRequest(ctx context.Context, owner, repo string, prID int64) (*PullRequest, error)
	GetPullRequestFiles(ctx context.Context, owner, repo string, prID int64) ([]PRFile, error)
	CreatePullRequestReview(ctx context.Context, owner, repo string, prID int64, body string, event string, comments []ReviewCommentInput) (int64, error)
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
	Name string `json:"name"`
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
	ID              int64  `json:"id"`
	Number          int64  `json:"number"`
	Title           string `json:"title"`
	Body            string `json:"body"`
	State           string `json:"state"` // open, closed
	User            User   `json:"user"`
	Head            Branch `json:"head"`
	Base            Branch `json:"base"`
	MergedAt        string `json:"merged_at,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	Draft           bool   `json:"draft"`
	CommitsCount    int    `json:"commits"`
	ChangedFiles    int    `json:"changed_files"`
	AdditionsCount  int    `json:"additions"`
	DeletionsCount  int    `json:"deletions"`
}

type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
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
		return ErrRateLimited
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusNoContent:
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github API error %d: %s", resp.StatusCode, string(raw))
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
