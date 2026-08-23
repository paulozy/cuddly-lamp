//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/testsupport/fakegitlab"
)

// The URL a user pastes decides which provider is queried; GITLAB_BASE_URL
// decides which host answers. That separation is what lets this suite run
// against a fake without pretending the repository lives somewhere else.
const (
	runnerURL = "https://gitlab.com/" + fakegitlab.RunnerPath
	hugeURL   = "https://gitlab.com/" + fakegitlab.HugePath
)

func configureGitLabToken(t *testing.T, token string) {
	t.Helper()
	sut.mustDo(t, http.MethodPatch, "/api/v1/organizations/configs", token, map[string]any{
		"gitlab_token": fakeGitLabToken,
	}, http.StatusOK)

	resp := sut.mustDo(t, http.MethodGet, "/api/v1/organizations/configs", token, nil, http.StatusOK)
	var cfg struct {
		GitlabTokenConfigured bool `json:"gitlab_token_configured"`
	}
	resp.decode(t, &cfg)
	if !cfg.GitlabTokenConfigured {
		t.Fatalf("organization config does not report the GitLab token as configured: %s", resp.body)
	}
}

// TestGitLabRepositoryLifecycle walks the whole path a GitLab repository takes:
// onboarding, credentials, catalog entry, sync, scorecard, webhook
// registration, and a delivery from the provider that refreshes the catalog.
func TestGitLabRepositoryLifecycle(t *testing.T) {
	token := sut.registerOrg(t, "lifecycle")
	configureGitLabToken(t, token)

	repo := sut.createRepository(t, token, runnerURL)
	if repo.Type != "gitlab" {
		t.Fatalf("type = %q, want gitlab", repo.Type)
	}
	// The nested group is part of the project's identity: truncated to
	// `gitlab-org/nested-group` it would name a different project.
	if repo.Name != fakegitlab.RunnerPath {
		t.Fatalf("name = %q, want %q", repo.Name, fakegitlab.RunnerPath)
	}

	synced := sut.waitForSync(t, token, repo.ID)
	if synced.SyncStatus != "synced" {
		t.Fatalf("sync_status = %q (error: %q), want synced", synced.SyncStatus, synced.SyncError)
	}

	t.Run("catalog is populated from the provider", func(t *testing.T) {
		meta := synced.Metadata
		if meta.ProviderID != "250833" {
			t.Errorf("provider_id = %q, want 250833", meta.ProviderID)
		}
		if meta.OwnerName != "gitlab-org/nested-group" {
			t.Errorf("owner_name = %q, want the full nested namespace", meta.OwnerName)
		}
		if meta.DefaultBranch != "main" {
			t.Errorf("default_branch = %q, want main", meta.DefaultBranch)
		}
		if meta.StarCount != 2569 || meta.ForkCount != 2651 {
			t.Errorf("stars/forks = %d/%d, want 2569/2651", meta.StarCount, meta.ForkCount)
		}
		if meta.BranchCount != 3 || meta.CommitCount != 2 {
			t.Errorf("branches/commits = %d/%d, want 3/2", meta.BranchCount, meta.CommitCount)
		}
		// Only opened merge requests count: the fixture also holds a merged one.
		if meta.PRCount != 2 {
			t.Errorf("pr_count = %d, want 2 open merge requests", meta.PRCount)
		}
	})

	t.Run("language percentages become integer weights", func(t *testing.T) {
		languages := synced.Metadata.Languages
		if languages["Go"] != 98 {
			t.Errorf("Go = %d, want 98 (rounded from 98.31)", languages["Go"])
		}
		// 0.14% must not round to zero, which would read as "language absent".
		if languages["HCL"] != 1 {
			t.Errorf("HCL = %d, want 1", languages["HCL"])
		}
	})

	t.Run("CI and tests are detected from a paginated tree", func(t *testing.T) {
		meta := synced.Metadata
		// `.gitlab-ci.yml` lives on the last page of the fixture's tree. This
		// assertion is what fails if the client stops paginating early.
		if meta.HasCI == nil || !*meta.HasCI {
			t.Errorf("has_ci = %v, want true", meta.HasCI)
		}
		if meta.HasTests == nil || !*meta.HasTests {
			t.Errorf("has_tests = %v, want true", meta.HasTests)
		}
		if meta.CIEvidence == "" || meta.TestEvidence == "" {
			t.Errorf("evidence = %q/%q, want the files that proved it", meta.CIEvidence, meta.TestEvidence)
		}
	})

	t.Run("scorecard reflects the synced facts", func(t *testing.T) {
		if synced.Scorecard == nil {
			t.Fatal("no scorecard on the repository response")
		}
		statuses := map[string]string{}
		for _, verdict := range synced.Scorecard.Verdicts {
			statuses[verdict.CheckID] = verdict.Status
		}
		for _, checkID := range []string{"sync.healthy", "delivery.has_ci", "quality.has_tests", "delivery.webhook_registered"} {
			if statuses[checkID] != "pass" {
				t.Errorf("%s = %q, want pass (verdicts: %v)", checkID, statuses[checkID], statuses)
			}
		}
		// Nothing uploaded coverage and no team owns it yet, so these must fail
		// rather than quietly pass.
		if statuses["quality.coverage_reported"] == "pass" {
			t.Error("quality.coverage_reported passed without any upload")
		}
		if statuses["ownership.has_owner_team"] == "pass" {
			t.Error("ownership.has_owner_team passed without an owner")
		}
	})

	t.Run("the sync registered a webhook with a secret", func(t *testing.T) {
		// By repository id, not "the last one registered": webhook
		// registration happens inside an asynchronous sync, so another test's
		// repository can land its hook in between.
		hook, ok := sut.fake.HookForRepo(repo.ID)
		if !ok {
			t.Fatalf("no webhook registered for repository %s", repo.ID)
		}
		if hook.ProjectPath != fakegitlab.RunnerPath {
			t.Errorf("hook project = %q, want %q", hook.ProjectPath, fakegitlab.RunnerPath)
		}
		wantSuffix := "/api/v1/webhooks/gitlab/" + repo.ID
		if !strings.HasSuffix(hook.URL, wantSuffix) {
			t.Errorf("hook URL = %q, want it to end in %q", hook.URL, wantSuffix)
		}
		// 32 random bytes, hex-encoded. A short or empty secret would mean
		// deliveries could not be authenticated.
		if len(hook.Token) != 64 {
			t.Errorf("hook token has %d chars, want 64 hex chars", len(hook.Token))
		}
		if !hook.PushEvents || !hook.MergeRequestsEvents {
			t.Errorf("hook = %+v, want push and merge request events", hook)
		}
	})

	t.Run("a push from the provider refreshes the catalog", func(t *testing.T) {
		before := sut.getRepository(t, token, repo.ID)
		if before.LastSyncedAt == nil {
			t.Fatal("repository has no last_synced_at after a successful sync")
		}
		// The stored timestamp has second granularity in places; make sure the
		// next sync lands on a distinguishable value.
		time.Sleep(1100 * time.Millisecond)

		status, err := sut.fake.FireWebhookTo(repo.ID, "Push Hook", "e2e-push-1", pushPayload())
		if err != nil {
			t.Fatalf("fire webhook: %v", err)
		}
		if status != http.StatusAccepted {
			t.Fatalf("receiver returned %d, want 202", status)
		}

		if err := waitFor(30*time.Second, func() error {
			current := sut.getRepository(t, token, repo.ID)
			if current.LastSyncedAt != nil && *current.LastSyncedAt != *before.LastSyncedAt {
				return nil
			}
			return fmt.Errorf("last_synced_at still %v", current.LastSyncedAt)
		}); err != nil {
			t.Fatalf("the push never triggered a new sync: %v", err)
		}
	})

	t.Run("a replayed delivery is ignored", func(t *testing.T) {
		// Same event UUID as above: GitLab retries, and a retry must not be
		// recorded twice.
		status, err := sut.fake.FireWebhookTo(repo.ID, "Push Hook", "e2e-push-1", pushPayload())
		if err != nil {
			t.Fatalf("fire webhook: %v", err)
		}
		if status != http.StatusOK {
			t.Fatalf("replay returned %d, want 200", status)
		}
	})

	t.Run("a delivery without the UUID header is still deduplicated", func(t *testing.T) {
		first, err := sut.fake.FireWebhookTo(repo.ID, "Push Hook", "", pushPayload())
		if err != nil {
			t.Fatalf("fire webhook: %v", err)
		}
		second, err := sut.fake.FireWebhookTo(repo.ID, "Push Hook", "", pushPayload())
		if err != nil {
			t.Fatalf("fire webhook: %v", err)
		}
		if first != http.StatusAccepted || second != http.StatusOK {
			t.Fatalf("statuses = %d/%d, want 202 then 200", first, second)
		}
	})

	t.Run("a merge request event is accepted and normalized", func(t *testing.T) {
		status, err := sut.fake.FireWebhookTo(repo.ID, "Merge Request Hook", "e2e-mr-1", mergeRequestPayload())
		if err != nil {
			t.Fatalf("fire webhook: %v", err)
		}
		if status != http.StatusAccepted {
			t.Fatalf("receiver returned %d, want 202", status)
		}
	})

	t.Run("a delivery with the wrong token is rejected", func(t *testing.T) {
		resp := sut.do(t, http.MethodPost, "/api/v1/webhooks/gitlab/"+repo.ID, "", pushPayload(),
			[2]string{"X-Gitlab-Token", "not-the-secret"},
			[2]string{"X-Gitlab-Event", "Push Hook"})
		if resp.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.status, resp.body)
		}
	})

	t.Run("a delivery with no token at all is rejected", func(t *testing.T) {
		resp := sut.do(t, http.MethodPost, "/api/v1/webhooks/gitlab/"+repo.ID, "", pushPayload(),
			[2]string{"X-Gitlab-Event", "Push Hook"})
		if resp.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.status, resp.body)
		}
	})

	t.Run("merge requests are browsable through the API", func(t *testing.T) {
		listResp := sut.mustDo(t, http.MethodGet, "/api/v1/repositories/"+repo.ID+"/pull-requests", token, nil, http.StatusOK)
		// Each item nests the change request under `pull_request`, which is the
		// shape the frontend already consumes.
		type listItem struct {
			PullRequest struct {
				Number     int64  `json:"number"`
				Title      string `json:"title"`
				State      string `json:"state"`
				Draft      bool   `json:"draft"`
				HeadBranch string `json:"head_branch"`
				BaseBranch string `json:"base_branch"`
				Author     string `json:"author_login"`
				HTMLURL    string `json:"html_url"`
			} `json:"pull_request"`
		}
		var list struct {
			Total int        `json:"total"`
			Items []listItem `json:"items"`
		}
		listResp.decode(t, &list)

		if list.Total != 2 {
			t.Fatalf("total = %d, want the 2 open merge requests\nbody: %s", list.Total, listResp.body)
		}
		byNumber := map[int64]int{}
		for i, item := range list.Items {
			byNumber[item.PullRequest.Number] = i
			// GitLab reports `opened`; the API must speak the vocabulary the
			// frontend already handles.
			if item.PullRequest.State != "open" {
				t.Errorf("MR %d state = %q, want open", item.PullRequest.Number, item.PullRequest.State)
			}
		}
		// iid, not the global id.
		if _, ok := byNumber[7222]; !ok {
			t.Fatalf("merge request 7222 missing from %v", byNumber)
		}
		if draft := list.Items[byNumber[7300]].PullRequest; !draft.Draft {
			t.Errorf("MR 7300 draft = false, want true")
		}
		if item := list.Items[byNumber[7222]].PullRequest; item.HeadBranch != "fix/doc-typo-catched-to-caught" || item.BaseBranch != "main" || item.Author != "pishel65" {
			t.Errorf("MR 7222 = %+v, want source/target branches and author mapped", item)
		}

		detailResp := sut.mustDo(t, http.MethodGet, "/api/v1/repositories/"+repo.ID+"/pull-requests/7222", token, nil, http.StatusOK)
		var detail struct {
			PullRequest struct {
				Number         int64  `json:"number"`
				BaseSHA        string `json:"base_sha"`
				HeadSHA        string `json:"head_sha"`
				ChangedFiles   int    `json:"changed_files"`
				AdditionsCount int    `json:"additions_count"`
				DeletionsCount int    `json:"deletions_count"`
			} `json:"pull_request"`
			Files []struct {
				Filename  string `json:"filename"`
				Status    string `json:"status"`
				Additions int    `json:"additions"`
				Deletions int    `json:"deletions"`
				Patch     string `json:"patch"`
			} `json:"files"`
		}
		detailResp.decode(t, &detail)

		if detail.PullRequest.BaseSHA == "" {
			t.Error("base_sha is empty — it comes from diff_refs on the detail payload")
		}
		// GitLab publishes no line counts on a merge request; these are summed
		// from the diffs, and zero would be a silent lie in the UI.
		if detail.PullRequest.AdditionsCount != 3 || detail.PullRequest.DeletionsCount != 1 {
			t.Errorf("additions/deletions = %d/%d, want 3/1",
				detail.PullRequest.AdditionsCount, detail.PullRequest.DeletionsCount)
		}
		if detail.PullRequest.ChangedFiles != 2 {
			t.Errorf("changed_files = %d, want 2", detail.PullRequest.ChangedFiles)
		}
		if len(detail.Files) != 2 {
			t.Fatalf("files = %d, want 2\nbody: %s", len(detail.Files), detailResp.body)
		}
		if detail.Files[0].Filename != "commands/multi.go" || detail.Files[0].Patch == "" {
			t.Errorf("first file = %+v, want the diff passed through", detail.Files[0])
		}
		if detail.Files[1].Status != "added" {
			t.Errorf("second file status = %q, want added", detail.Files[1].Status)
		}

		filesResp := sut.mustDo(t, http.MethodGet, "/api/v1/repositories/"+repo.ID+"/pull-requests/7222/files", token, nil, http.StatusOK)
		var files struct {
			Total int `json:"total"`
		}
		filesResp.decode(t, &files)
		if files.Total != 2 {
			t.Errorf("files endpoint total = %d, want 2", files.Total)
		}
	})

	t.Run("an unknown merge request is a 404, not a 500", func(t *testing.T) {
		resp := sut.do(t, http.MethodGet, "/api/v1/repositories/"+repo.ID+"/pull-requests/999999", token, nil)
		if resp.status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404\nbody: %s", resp.status, resp.body)
		}
	})
}

// TestTruncatedTreeLeavesDetectionUnknown covers the case that separates an
// honest catalog from a misleading one: a repository too large to list fully
// must report "not verified", never a confident "no CI".
func TestTruncatedTreeLeavesDetectionUnknown(t *testing.T) {
	token := sut.registerOrg(t, "truncated")
	configureGitLabToken(t, token)

	repo := sut.createRepository(t, token, hugeURL)
	synced := sut.waitForSync(t, token, repo.ID)

	if synced.SyncStatus != "synced" {
		t.Fatalf("sync_status = %q (error: %q), want synced", synced.SyncStatus, synced.SyncError)
	}
	if synced.Metadata.HasCI != nil {
		t.Errorf("has_ci = %v, want nil (unknown) on a truncated listing", *synced.Metadata.HasCI)
	}
	if synced.Metadata.HasTests != nil {
		t.Errorf("has_tests = %v, want nil (unknown) on a truncated listing", *synced.Metadata.HasTests)
	}

	if synced.Scorecard == nil {
		t.Fatal("no scorecard on the repository response")
	}
	for _, verdict := range synced.Scorecard.Verdicts {
		if verdict.CheckID != "delivery.has_ci" {
			continue
		}
		// "We could not see the whole repository" is not the same claim as
		// "this repository has no CI", and the scorecard must not conflate them.
		if verdict.Status == "fail" {
			t.Errorf("delivery.has_ci = fail on an unverifiable repository (reason: %q)", verdict.Reason)
		}
	}
}

// TestProviderErrorsAreReportedClearly checks the failure paths a user actually
// hits, since before GitLab existed every one of them said "only github".
func TestProviderErrorsAreReportedClearly(t *testing.T) {
	token := sut.registerOrg(t, "errors")

	t.Run("a GitLab repository without a token fails on credentials", func(t *testing.T) {
		// This organization never configured a token, and the deployment's
		// GITLAB_TOKEN is empty in this run.
		repo := sut.createRepository(t, token, runnerURL)

		// Read the detail endpoint before the sync settles, which is what puts
		// the pre-sync state in the cache. Regression: only a successful sync
		// invalidated it, so this read used to pin `idle` for an hour and the
		// failure below stayed invisible on the repository page.
		if before := sut.getRepository(t, token, repo.ID); before.SyncStatus == "synced" {
			t.Fatalf("sync_status = %q before any provider call", before.SyncStatus)
		}

		synced := sut.waitForSync(t, token, repo.ID)

		if synced.SyncStatus != "error" {
			t.Fatalf("sync_status = %q, want error", synced.SyncStatus)
		}
		if !strings.Contains(synced.SyncError, "credentials") {
			t.Errorf("sync_error = %q, want it to name the missing credentials", synced.SyncError)
		}
		// The catalog row must not be populated from anywhere else.
		if synced.Metadata.DefaultBranch != "" {
			t.Errorf("metadata was populated despite the failure: %+v", synced.Metadata)
		}

		// The cached detail endpoint has to agree with the list. Disagreeing
		// means the repository page shows "never synced" for a repository whose
		// sync failed — a different problem with a different fix.
		detail := sut.getRepository(t, token, repo.ID)
		if detail.SyncStatus != "error" {
			t.Errorf("detail sync_status = %q, want error (list said %q)", detail.SyncStatus, synced.SyncStatus)
		}
		if detail.SyncError == "" {
			t.Error("detail response carries no sync_error")
		}
	})

	t.Run("a host with no client is refused as unsupported", func(t *testing.T) {
		repo := sut.createRepository(t, token, "https://gitea.example.com/owner/repo")
		synced := sut.waitForSync(t, token, repo.ID)

		if synced.SyncStatus != "error" {
			t.Fatalf("sync_status = %q, want error", synced.SyncStatus)
		}
		if !strings.Contains(synced.SyncError, "unsupported provider") {
			t.Errorf("sync_error = %q, want it to say the provider is unsupported", synced.SyncError)
		}
	})

	t.Run("browsing merge requests without a token is a 503", func(t *testing.T) {
		repo := sut.createRepository(t, token, "https://gitlab.com/"+fakegitlab.HugePath)
		resp := sut.do(t, http.MethodGet, "/api/v1/repositories/"+repo.ID+"/pull-requests", token, nil)
		if resp.status != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503\nbody: %s", resp.status, resp.body)
		}
	})

	t.Run("another organization cannot reach this repository", func(t *testing.T) {
		other := sut.registerOrg(t, "intruder")
		repo := sut.createRepository(t, token, "https://gitlab.com/gitlab-org/nested-group/private-thing")

		resp := sut.do(t, http.MethodGet, "/api/v1/repositories/"+repo.ID+"/pull-requests", other, nil)
		if resp.status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403\nbody: %s", resp.status, resp.body)
		}
	})

	t.Run("a webhook for a repository with no configuration is rejected", func(t *testing.T) {
		repo := sut.createRepository(t, token, "https://gitlab.com/gitlab-org/nested-group/unhooked")
		resp := sut.do(t, http.MethodPost, "/api/v1/webhooks/gitlab/"+repo.ID, "", pushPayload(),
			[2]string{"X-Gitlab-Token", "anything"},
			[2]string{"X-Gitlab-Event", "Push Hook"})
		if resp.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.status, resp.body)
		}
	})
}

// TestDocumentationRequiresTheRepositorysOwnProvider covers the precondition
// the docs handler checks before enqueueing anything.
func TestDocumentationRequiresTheRepositorysOwnProvider(t *testing.T) {
	token := sut.registerOrg(t, "docs")
	configureGitLabToken(t, token)
	repo := sut.createRepository(t, token, runnerURL)

	// No Anthropic key in this run, so generation is unavailable — but the
	// point is that it fails on the AI key, not on a GitHub token a GitLab
	// repository has no use for.
	resp := sut.do(t, http.MethodPost, "/api/v1/repositories/"+repo.ID+"/docs/generate", token,
		map[string]any{"types": []string{"architecture"}})
	if resp.status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503\nbody: %s", resp.status, resp.body)
	}
	if strings.Contains(strings.ToLower(string(resp.body)), "github") {
		t.Errorf("a GitLab repository was refused over GitHub credentials: %s", resp.body)
	}
}

// ── payload fixtures ─────────────────────────────────────────────────────────

func pushPayload() map[string]any {
	return map[string]any{
		"object_kind":  "push",
		"ref":          "refs/heads/main",
		"checkout_sha": "e2e-push-sha",
		"user_name":    "E2E",
		"project": map[string]any{
			"id":                  250833,
			"path_with_namespace": fakegitlab.RunnerPath,
		},
	}
}

func mergeRequestPayload() map[string]any {
	return map[string]any{
		"object_kind": "merge_request",
		"project": map[string]any{
			"id":                  250833,
			"path_with_namespace": fakegitlab.RunnerPath,
		},
		"user": map[string]any{"username": "dev"},
		"object_attributes": map[string]any{
			"iid":           7222,
			"action":        "update",
			"source_branch": "fix/doc-typo-catched-to-caught",
			"target_branch": "main",
			"last_commit":   map[string]any{"id": "d094172f", "message": "fix: typo"},
		},
	}
}
