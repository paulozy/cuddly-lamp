package github

import (
	"context"
	"fmt"
	"net/url"
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

// TreeEntry is one node of a repository tree listing.
type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
	Size int    `json:"size"`
}

// RepoTree is a recursive listing of a repository at a ref.
//
// Truncated matters: GitHub caps the recursive response, and when it trips the
// listing is incomplete. Absence of a path in a truncated tree proves nothing,
// so callers must treat "not found" as unknown rather than as "does not exist".
type RepoTree struct {
	SHA       string      `json:"sha"`
	Truncated bool        `json:"truncated"`
	Tree      []TreeEntry `json:"tree"`
}

// GetRepositoryTree fetches the full recursive file listing at a ref in a
// single request. `ref` may be a branch name — the API resolves it.
//
// One call replaces the dozens of Contents API requests that walking the tree
// by hand would need, which matters because this runs on every sync.
func (c *Client) GetRepositoryTree(ctx context.Context, owner, repo, ref string) (*RepoTree, error) {
	path := fmt.Sprintf("/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, url.PathEscape(ref))
	var tree RepoTree
	if err := c.do(ctx, "GET", path, nil, &tree); err != nil {
		return nil, err
	}
	return &tree, nil
}

// BlobPaths returns only the file paths, dropping directory entries. Detection
// works on files: a `test/` directory that contains no files proves nothing.
func (t *RepoTree) BlobPaths() []string {
	paths := make([]string, 0, len(t.Tree))
	for i := range t.Tree {
		if t.Tree[i].Type == "blob" {
			paths = append(paths, t.Tree[i].Path)
		}
	}
	return paths
}

// GetLanguages returns the repository's language byte counts.
//
// This is the real breakdown, unlike `RepoInfo.Language`, which is only the
// single dominant language. Test detection gates on the full set — a Go service
// with a Cypress suite needs JavaScript to be visible — so the single value is
// not enough.
func (c *Client) GetLanguages(ctx context.Context, owner, repo string) (map[string]int, error) {
	var languages map[string]int
	path := fmt.Sprintf("/repos/%s/%s/languages", owner, repo)
	if err := c.do(ctx, "GET", path, nil, &languages); err != nil {
		return nil, err
	}
	return languages, nil
}
