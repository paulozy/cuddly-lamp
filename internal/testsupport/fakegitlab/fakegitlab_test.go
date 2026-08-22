package fakegitlab_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/gitlab"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/testsupport/fakegitlab"
)

// A fake is only worth having if the real client understands it. These tests
// drive the production client and adapter against the fake, so a fixture that
// drifts from what the client expects fails here rather than silently making
// the end-to-end suite pass against a fiction.

const token = "glpat-fake-token"

func newFake(t *testing.T) (*fakegitlab.Server, scm.Provider) {
	t.Helper()
	fake := fakegitlab.Default(token)
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)

	provider, err := scm.For(models.RepositoryTypeGitLab, scm.Credentials{
		GitLabToken:   token,
		GitLabBaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("resolve provider: %v", err)
	}
	return fake, provider
}

func runnerRef(t *testing.T) scm.RepoRef {
	t.Helper()
	ref, err := scm.ParseRepoRef(fakegitlab.RunnerPath)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}
	return ref
}

func TestFake_RejectsAWrongToken(t *testing.T) {
	fake := fakegitlab.Default(token)
	srv := httptest.NewServer(fake.Handler())
	defer srv.Close()

	client := gitlab.NewClientWithBaseURL("wrong-token", srv.URL)
	if _, err := client.GetProject(context.Background(), fakegitlab.RunnerPath); err != gitlab.ErrUnauthorized {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestFake_ServesANestedProject(t *testing.T) {
	_, provider := newFake(t)

	info, err := provider.GetRepository(context.Background(), runnerRef(t))
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if info.FullName != fakegitlab.RunnerPath {
		t.Errorf("FullName = %q, want the nested path", info.FullName)
	}
	if info.StarCount != 2569 || info.DefaultBranch != "main" {
		t.Errorf("info = %+v, want the fixture's counts", info)
	}
	// The fixture sends open_issues_count: null, as gitlab.com does.
	if info.OpenIssueCount != 0 {
		t.Errorf("OpenIssueCount = %d, want 0 for a null field", info.OpenIssueCount)
	}
}

func TestFake_TreePaginationCarriesLateRootFiles(t *testing.T) {
	_, provider := newFake(t)

	tree, err := provider.GetTree(context.Background(), runnerRef(t), "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if tree.Truncated {
		t.Error("Truncated = true, but the fixture has a last page")
	}
	var sawCI, sawTest bool
	for _, path := range tree.BlobPaths() {
		switch path {
		case ".gitlab-ci.yml":
			sawCI = true
		case "commands/multi_test.go":
			sawTest = true
		}
	}
	// `.gitlab-ci.yml` sits on the last page on purpose: this is the assertion
	// that would fail if the client stopped paginating early.
	if !sawCI || !sawTest {
		t.Fatalf("blob paths = %v, want the CI file from the last page and the test file", tree.BlobPaths())
	}
}

func TestFake_EndlessTreeMakesTheClientReportTruncation(t *testing.T) {
	_, provider := newFake(t)
	ref, err := scm.ParseRepoRef(fakegitlab.HugePath)
	if err != nil {
		t.Fatalf("parse ref: %v", err)
	}

	tree, err := provider.GetTree(context.Background(), ref, "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if !tree.Truncated {
		t.Fatal("Truncated = false against an endless tree")
	}
}

func TestFake_MergeRequestsMatchWhatTheClientExpects(t *testing.T) {
	_, provider := newFake(t)
	ref := runnerRef(t)

	open, err := provider.ListChangeRequests(context.Background(), ref)
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	// The merged one must not appear: the client asks for state=opened.
	if len(open) != 2 {
		t.Fatalf("open change requests = %d, want 2", len(open))
	}
	for _, cr := range open {
		if cr.State != scm.ChangeRequestStateOpen {
			t.Errorf("state = %q, want open", cr.State)
		}
	}

	detail, err := provider.GetChangeRequest(context.Background(), ref, 7222)
	if err != nil {
		t.Fatalf("GetChangeRequest: %v", err)
	}
	if detail.Number != 7222 || detail.BaseSHA == "" {
		t.Errorf("detail = %+v, want iid 7222 and a base sha from diff_refs", detail)
	}
	// Counted from the diff bodies, because GitLab reports no line counts.
	if detail.Additions != 3 || detail.Deletions != 1 || detail.ChangedFiles != 2 {
		t.Errorf("additions/deletions/files = %d/%d/%d, want 3/1/2", detail.Additions, detail.Deletions, detail.ChangedFiles)
	}

	files, err := provider.GetChangeRequestFiles(context.Background(), ref, 7222)
	if err != nil {
		t.Fatalf("GetChangeRequestFiles: %v", err)
	}
	if len(files) != 2 || files[1].Status != scm.FileStatusAdded {
		t.Fatalf("files = %+v, want the second one added", files)
	}
}

func TestFake_RecordsWritesTheDocsWorkerPerforms(t *testing.T) {
	fake, provider := newFake(t)
	ref := runnerRef(t)
	ctx := context.Background()

	if err := provider.CreateBranch(ctx, ref, "main", "docs/auto-generated-1"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := provider.UpsertFile(ctx, ref, "docs/auto-generated-1", "docs/ARCHITECTURE.md", "docs: add", "# Arch"); err != nil {
		t.Fatalf("UpsertFile (create): %v", err)
	}
	if err := provider.UpsertFile(ctx, ref, "docs/auto-generated-1", "docs/ARCHITECTURE.md", "docs: update", "# Arch v2"); err != nil {
		t.Fatalf("UpsertFile (update): %v", err)
	}
	cr, err := provider.OpenChangeRequest(ctx, ref, "docs: auto-generate", "docs/auto-generated-1", "main", "body")
	if err != nil {
		t.Fatalf("OpenChangeRequest: %v", err)
	}
	if cr.Number == 0 || cr.WebURL == "" {
		t.Errorf("change request = %+v, want a number and URL to store on the doc row", cr)
	}

	if branches := fake.Branches(); len(branches) != 1 || branches[0].Ref != "main" {
		t.Fatalf("branches = %+v, want one branched off main", branches)
	}
	files := fake.Files()
	if len(files) != 2 {
		t.Fatalf("files = %+v, want two writes", files)
	}
	// First write creates, second updates: the client probes for existence and
	// switches verbs, because POSTing over an existing file fails on GitLab.
	if files[0].Method != "POST" || files[1].Method != "PUT" {
		t.Errorf("methods = %s/%s, want POST then PUT", files[0].Method, files[1].Method)
	}
	if files[1].Content != "# Arch v2" {
		t.Errorf("content = %q, want the updated markdown", files[1].Content)
	}
	if opened := fake.MergeRequestsOpened(); len(opened) != 1 || opened[0].SourceBranch != "docs/auto-generated-1" {
		t.Fatalf("opened = %+v, want the docs branch", opened)
	}
}

func TestFake_RecordsWebhookRegistration(t *testing.T) {
	fake, provider := newFake(t)

	hook, err := provider.RegisterWebhook(context.Background(), runnerRef(t), "https://idp.example/api/v1/webhooks/gitlab/repo-1", "s3cret")
	if err != nil {
		t.Fatalf("RegisterWebhook: %v", err)
	}
	if hook.ID == "" {
		t.Error("hook ID is empty — it is stored as provider_webhook_id")
	}

	hooks := fake.Hooks()
	if len(hooks) != 1 {
		t.Fatalf("hooks = %d, want 1", len(hooks))
	}
	// The secret must reach the provider, or deliveries can never be
	// authenticated.
	if hooks[0].Token != "s3cret" || !hooks[0].PushEvents || !hooks[0].MergeRequestsEvents {
		t.Fatalf("hook = %+v, want the secret and both event subscriptions", hooks[0])
	}
	if !hooks[0].SSLVerification {
		t.Error("SSL verification was disabled")
	}
}
