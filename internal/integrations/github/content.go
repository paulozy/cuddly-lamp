package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type gitRefResponse struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type fileContentResponse struct {
	SHA string `json:"sha"`
}

func (c *Client) CreateBranch(ctx context.Context, owner, repo, baseBranch, newBranch string) error {
	baseRefPath := fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s", owner, repo, baseBranch)
	var baseRef gitRefResponse
	if err := c.do(ctx, "GET", baseRefPath, nil, &baseRef); err != nil {
		return err
	}
	if baseRef.Object.SHA == "" {
		return fmt.Errorf("github: base branch %q has no sha", baseBranch)
	}

	reqBody := map[string]string{
		"ref": "refs/heads/" + newBranch,
		"sha": baseRef.Object.SHA,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal create branch request: %w", err)
	}
	var out gitRefResponse
	path := fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo)
	return c.do(ctx, "POST", path, bytes.NewReader(bodyBytes), &out)
}

func (c *Client) CreateOrUpdateFile(ctx context.Context, owner, repo, branch, path, message, content string) error {
	sha, err := c.getFileSHA(ctx, owner, repo, branch, path)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}

	reqBody := map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  branch,
	}
	if sha != "" {
		reqBody["sha"] = sha
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal file request: %w", err)
	}

	putPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)
	return c.do(ctx, "PUT", putPath, bytes.NewReader(bodyBytes), nil)
}

func (c *Client) getFileSHA(ctx context.Context, owner, repo, branch, path string) (string, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s?ref=%s", owner, repo, path, url.QueryEscape(branch))
	var resp fileContentResponse
	if err := c.do(ctx, "GET", apiPath, nil, &resp); err != nil {
		return "", err
	}
	return resp.SHA, nil
}

// GetFileContent returns a file's bytes at ref, reading at most limit bytes.
//
// `application/vnd.github.raw+json` makes the contents endpoint answer with the
// file itself instead of a base64 envelope, which is what keeps this a single
// request with no decoding step. The endpoint serves files up to 1 MB in every
// mode and up to 100 MB in raw mode, so the caller's limit is the binding one.
// https://docs.github.com/en/rest/repos/contents
func (c *Client) GetFileContent(ctx context.Context, owner, repo, ref, path string, limit int64) ([]byte, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, escapeContentPath(path))
	if ref != "" {
		apiPath += "?ref=" + url.QueryEscape(ref)
	}
	return c.doRaw(ctx, apiPath, "application/vnd.github.raw+json", limit)
}

// escapeContentPath percent-encodes each path segment while keeping the
// separators, because the contents endpoint addresses the file by path.
func escapeContentPath(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}
