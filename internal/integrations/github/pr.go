package github

import (
	"context"
	"fmt"
)

// PRFile represents a file changed in a PR
type PRFile struct {
	SHA       string `json:"sha"`
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added, modified, removed, renamed, copied, changed, unchanged
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch"` // Unified diff
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, prID int64) (*PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, prID)
	var pr PullRequest
	if err := c.do(ctx, "GET", path, nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// GetPullRequestFiles fetches the list of files changed in a PR
func (c *Client) GetPullRequestFiles(ctx context.Context, owner, repo string, prID int64) ([]PRFile, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", owner, repo, prID)
	var files []PRFile
	if err := c.do(ctx, "GET", path, nil, &files); err != nil {
		return nil, err
	}
	return files, nil
}
