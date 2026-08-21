package gitlab

import (
	"context"
	"os"
	"strings"
	"testing"
)

// These tests talk to the real gitlab.com API, anonymously, against a public
// project. They are opt-in because they need network access.
//
// They exist because the unit tests above assert against payload shapes that
// this file is the only thing keeping honest: if GitLab renames a field or
// changes how the tree paginates, only a real request notices.
//
//	GITLAB_LIVE_TEST=1 go test ./internal/integrations/gitlab/ -run Live -v
const liveProject = "gitlab-org/gitlab-runner"

func liveClient(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("GITLAB_LIVE_TEST") == "" {
		t.Skip("GITLAB_LIVE_TEST not set — skipping live gitlab.com test")
	}
	// Empty token: public projects are readable anonymously, so this needs no
	// credential to run.
	return NewClient(os.Getenv("GITLAB_TEST_TOKEN"))
}

func TestLive_GetProject(t *testing.T) {
	project, err := liveClient(t).GetProject(context.Background(), liveProject)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if project.ID == 0 || project.PathWithNamespace != liveProject {
		t.Fatalf("project = %+v, want id and path populated", project)
	}
	if project.DefaultBranch == "" {
		t.Error("DefaultBranch is empty — sync depends on it")
	}
}

func TestLive_GetLanguagesArePercentages(t *testing.T) {
	languages, err := liveClient(t).GetLanguages(context.Background(), liveProject)
	if err != nil {
		t.Fatalf("GetLanguages: %v", err)
	}
	if len(languages) == 0 {
		t.Fatal("no languages returned")
	}
	var total float64
	for _, percent := range languages {
		total += percent
	}
	// Percentages, not byte counts: the normalizer in the scm adapter depends
	// on this and would produce nonsense weights if GitLab switched to bytes.
	if total < 99 || total > 101 {
		t.Fatalf("language values sum to %.2f, want ~100 (percentages)", total)
	}
}

func TestLive_GetTreePaginates(t *testing.T) {
	tree, err := liveClient(t).GetTree(context.Background(), liveProject, "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	// gitlab-runner is well past one page, so anything under 200 entries means
	// pagination silently stopped.
	if len(tree.Entries) < 200 {
		t.Fatalf("entries = %d, want the walk to follow x-next-page", len(tree.Entries))
	}
	var sawCI bool
	for _, entry := range tree.Entries {
		if entry.Path == ".gitlab-ci.yml" {
			sawCI = true
		}
	}
	// The recursive listing is depth-first, so a root-level file lands on a
	// late page. Missing it here would mean CI detection reads as "no CI".
	if !sawCI && !tree.Truncated {
		t.Error(".gitlab-ci.yml not found in a complete tree listing")
	}
}

func TestLive_MergeRequestsAndDiffs(t *testing.T) {
	client := liveClient(t)
	mrs, err := client.ListMergeRequests(context.Background(), liveProject)
	if err != nil {
		t.Fatalf("ListMergeRequests: %v", err)
	}
	if len(mrs) == 0 {
		t.Skip("no open merge requests right now")
	}
	if mrs[0].IID == 0 || mrs[0].State != "opened" {
		t.Fatalf("merge request = %+v, want iid and state=opened", mrs[0])
	}

	detail, err := client.GetMergeRequest(context.Background(), liveProject, mrs[0].IID)
	if err != nil {
		t.Fatalf("GetMergeRequest: %v", err)
	}
	// base_sha lives only on the detail payload; the list omits diff_refs.
	if detail.DiffRefs.BaseSHA == "" {
		t.Error("diff_refs.base_sha is empty on the merge request detail")
	}

	diffs, err := client.ListMergeRequestDiffs(context.Background(), liveProject, mrs[0].IID)
	if err != nil {
		t.Fatalf("ListMergeRequestDiffs: %v", err)
	}
	if len(diffs) == 0 {
		t.Fatal("no diffs returned for an open merge request")
	}
	if diffs[0].NewPath == "" {
		t.Errorf("diff = %+v, want new_path", diffs[0])
	}
	// The diff body is what the UI renders as a patch; GitLab starts it at the
	// hunk header.
	if diffs[0].Diff != "" && !strings.Contains(diffs[0].Diff, "@@") {
		t.Errorf("diff body %q does not look like a unified diff", diffs[0].Diff)
	}
}
