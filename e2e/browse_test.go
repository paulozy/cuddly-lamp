//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Issue and contributor browsing, driven through the real HTTP stack against
// the fake GitLab. These are the paths where a wrong translation is invisible
// to unit tests: GitLab says `opened` and addresses issues by `iid`, and its
// contributors carry a display name and no username.

type issueListResponse struct {
	Items []struct {
		Number        int64    `json:"number"`
		Title         string   `json:"title"`
		State         string   `json:"state"`
		AuthorLogin   string   `json:"author_login"`
		Labels        []string `json:"labels"`
		CommentsCount int      `json:"comments_count"`
	} `json:"items"`
	Total int `json:"total"`
}

type contributorListResponse struct {
	Items []struct {
		Login              string  `json:"login"`
		Name               string  `json:"name"`
		Commits            int     `json:"commits"`
		OpenChangeRequests *int    `json:"open_change_requests"`
		LastCommitAt       *string `json:"last_commit_at"`
	} `json:"items"`
	Total int `json:"total"`
}

func TestGitLabIssueBrowsingAndClosing(t *testing.T) {
	token := sut.registerOrg(t, "issues")
	configureGitLabToken(t, token)
	repo := sut.createRepository(t, token, runnerURL)

	resp := sut.mustDo(t, http.MethodGet,
		fmt.Sprintf("/api/v1/repositories/%s/issues", repo.ID), token, nil, http.StatusOK)

	var list issueListResponse
	if err := json.Unmarshal([]byte(resp.body), &list); err != nil {
		t.Fatalf("decode issues: %v\nbody: %s", err, resp.body)
	}
	if list.Total != 2 {
		t.Fatalf("total = %d, want 2\nbody: %s", list.Total, resp.body)
	}

	first := list.Items[0]
	// The per-project iid, not the global id.
	if first.Number != 88 {
		t.Errorf("number = %d, want 88 (the iid)", first.Number)
	}
	// GitLab reports `opened`; the platform's vocabulary is GitHub's.
	if first.State != "open" {
		t.Errorf("state = %q, want open", first.State)
	}
	if first.AuthorLogin != "julia.r" {
		t.Errorf("author_login = %q, want julia.r", first.AuthorLogin)
	}
	if len(first.Labels) != 2 {
		t.Errorf("labels = %v, want two", first.Labels)
	}
	if first.CommentsCount != 3 {
		t.Errorf("comments_count = %d, want 3", first.CommentsCount)
	}

	// Closing must reach the provider, and the closed issue must drop out of
	// the next listing.
	sut.mustDo(t, http.MethodPost,
		fmt.Sprintf("/api/v1/repositories/%s/issues/88/close", repo.ID), token, nil, http.StatusNoContent)

	closed := sut.fake.ClosedIssues()
	if len(closed) != 1 || closed[0] != 88 {
		t.Fatalf("provider recorded %v closed, want [88]", closed)
	}

	resp = sut.mustDo(t, http.MethodGet,
		fmt.Sprintf("/api/v1/repositories/%s/issues", repo.ID), token, nil, http.StatusOK)
	var after issueListResponse
	json.Unmarshal([]byte(resp.body), &after)
	if after.Total != 1 {
		t.Errorf("total after close = %d, want 1", after.Total)
	}
}

func TestGitLabContributorBrowsing(t *testing.T) {
	token := sut.registerOrg(t, "contributors")
	configureGitLabToken(t, token)
	repo := sut.createRepository(t, token, runnerURL)

	resp := sut.mustDo(t, http.MethodGet,
		fmt.Sprintf("/api/v1/repositories/%s/contributors", repo.ID), token, nil, http.StatusOK)

	var list contributorListResponse
	if err := json.Unmarshal([]byte(resp.body), &list); err != nil {
		t.Fatalf("decode contributors: %v\nbody: %s", err, resp.body)
	}
	if list.Total != 2 {
		t.Fatalf("total = %d, want 2\nbody: %s", list.Total, resp.body)
	}

	top := list.Items[0]
	if top.Name != "Vishal Tak" {
		t.Errorf("name = %q, want Vishal Tak", top.Name)
	}
	// GitLab's contributors endpoint carries no username; inventing one from
	// the email would be a guess.
	if top.Login != "" {
		t.Errorf("login = %q, want empty for a GitLab contributor", top.Login)
	}
	if top.Commits != 240 {
		t.Errorf("commits = %d, want 240", top.Commits)
	}
	// Derived by matching the display name against the recent commit window.
	if top.LastCommitAt == nil {
		t.Error("last_commit_at = null, want a date matched from the commit log")
	}
}

// A repository whose organization has no token for its host cannot be browsed,
// and that must read as "the integration cannot answer" rather than as an
// empty repository.
func TestIssueBrowsingWithoutProviderCredentials(t *testing.T) {
	token := sut.registerOrg(t, "issues-no-token")
	repo := sut.createRepository(t, token, runnerURL)

	sut.mustDo(t, http.MethodGet,
		fmt.Sprintf("/api/v1/repositories/%s/issues", repo.ID), token, nil, http.StatusServiceUnavailable)
}

// GitLab can approve a merge request but has no portable "request changes".
// The two must not collapse into one answer.
func TestGitLabReviewVerdicts(t *testing.T) {
	token := sut.registerOrg(t, "review")
	configureGitLabToken(t, token)
	repo := sut.createRepository(t, token, runnerURL)

	sut.mustDo(t, http.MethodPost,
		fmt.Sprintf("/api/v1/repositories/%s/pull-requests/1/approve", repo.ID), token,
		map[string]any{"body": "Looks good."}, http.StatusNoContent)

	approved := sut.fake.ApprovedMergeRequests()
	if len(approved) != 1 || approved[0] != 1 {
		t.Fatalf("provider recorded %v approved, want [1]", approved)
	}

	// 501, not 503: the host is reachable, it simply has no such action.
	sut.mustDo(t, http.MethodPost,
		fmt.Sprintf("/api/v1/repositories/%s/pull-requests/1/request-changes", repo.ID), token,
		map[string]any{"body": ""}, http.StatusNotImplemented)
}
