package scm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newGitLabProvider(srv *httptest.Server) Provider {
	return NewGitLabProviderWithBaseURL("test-token", srv.URL)
}

func TestGitLabProvider_MapsProjectOntoNeutralCatalogFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":                  250833,
			"name":                "project",
			"path_with_namespace": "group/subgroup/project",
			"default_branch":      "main",
			"star_count":          12,
			"forks_count":         3,
			"open_issues_count":   nil,
			"visibility":          "private",
		})
	}))
	defer srv.Close()

	info, err := newGitLabProvider(srv).GetRepository(context.Background(), RepoRef{Namespace: "group/subgroup", Name: "project"})
	if err != nil {
		t.Fatalf("GetRepository: %v", err)
	}
	if info.ID != 250833 || info.StarCount != 12 || info.ForkCount != 3 || info.DefaultBranch != "main" {
		t.Errorf("info = %+v, want the GitLab counts mapped", info)
	}
	// A null open_issues_count means "issues disabled", not "issues at zero";
	// neither can be expressed better than 0 here, but it must not panic.
	if info.OpenIssueCount != 0 {
		t.Errorf("OpenIssueCount = %d, want 0 for an absent count", info.OpenIssueCount)
	}
	if !info.Private {
		t.Error("Private = false for a non-public project")
	}
	// GitLab reports no dominant language on the project; claiming one would
	// invent data, so it stays empty and ListLanguages is authoritative.
	if info.Language != "" {
		t.Errorf("Language = %q, want empty", info.Language)
	}
}

func TestGitLabProvider_NormalizesLanguagePercentages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"Go":98.31,"Shell":0.91,"HCL":0.14}`))
	}))
	defer srv.Close()

	languages, err := newGitLabProvider(srv).ListLanguages(context.Background(), RepoRef{Namespace: "owner", Name: "repo"})
	if err != nil {
		t.Fatalf("ListLanguages: %v", err)
	}
	if languages["Go"] != 98 || languages["Shell"] != 1 {
		t.Errorf("languages = %v, want percentages rounded to integer weights", languages)
	}
	// Detection asks which languages are present. Rounding 0.14 down to 0
	// would leave the key in the map with a weight that reads as absent.
	if languages["HCL"] != 1 {
		t.Errorf("HCL = %d, want a present language to keep a non-zero weight", languages["HCL"])
	}
}

func TestGitLabProvider_NormalizesMergeRequestStates(t *testing.T) {
	tests := []struct {
		gitlabState string
		want        string
	}{
		{gitlabState: "opened", want: ChangeRequestStateOpen},
		// A locked merge request is still open — it just cannot be commented on.
		{gitlabState: "locked", want: ChangeRequestStateOpen},
		{gitlabState: "closed", want: ChangeRequestStateClosed},
		// GitHub reports a merged PR as closed with merged_at set, and the
		// frontend already speaks that vocabulary.
		{gitlabState: "merged", want: ChangeRequestStateClosed},
	}

	for _, tt := range tests {
		t.Run(tt.gitlabState, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode([]map[string]any{{
					"id":    1,
					"iid":   42,
					"state": tt.gitlabState,
				}})
			}))
			defer srv.Close()

			crs, err := newGitLabProvider(srv).ListChangeRequests(context.Background(), RepoRef{Namespace: "owner", Name: "repo"})
			if err != nil {
				t.Fatalf("ListChangeRequests: %v", err)
			}
			if len(crs) != 1 || crs[0].State != tt.want {
				t.Fatalf("state = %q, want %q", crs[0].State, tt.want)
			}
		})
	}
}

func TestGitLabProvider_UsesIIDAsTheChangeRequestNumber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{{
			"id":            522266146, // global, meaningless in a URL
			"iid":           7222,      // what users and API paths use
			"state":         "opened",
			"draft":         true,
			"source_branch": "fix/typo",
			"target_branch": "main",
			"sha":           "d094172f",
			"web_url":       "https://gitlab.com/group/project/-/merge_requests/7222",
			"author":        map[string]any{"username": "dev"},
		}})
	}))
	defer srv.Close()

	crs, err := newGitLabProvider(srv).ListChangeRequests(context.Background(), RepoRef{Namespace: "group", Name: "project"})
	if err != nil {
		t.Fatalf("ListChangeRequests: %v", err)
	}
	cr := crs[0]
	// Using the global ID here would 404 on every follow-up request.
	if cr.Number != 7222 || cr.ID != 522266146 {
		t.Errorf("number/id = %d/%d, want 7222/522266146", cr.Number, cr.ID)
	}
	if cr.HeadRef != "fix/typo" || cr.BaseRef != "main" || cr.AuthorLogin != "dev" || !cr.Draft {
		t.Errorf("change request = %+v, want branches, author and draft mapped", cr)
	}
}

func TestGitLabProvider_GetChangeRequestSumsDiffLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/group%2Fproject/merge_requests/7222" ||
			r.URL.Path == "/projects/group/project/merge_requests/7222" {
			json.NewEncoder(w).Encode(map[string]any{
				"id":            1,
				"iid":           7222,
				"state":         "opened",
				"sha":           "head-sha",
				"changes_count": "2",
				"diff_refs":     map[string]any{"base_sha": "base-sha", "head_sha": "head-sha"},
			})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"new_path": "a.go", "diff": "@@ -1 +1,3 @@\n context\n+added one\n+added two\n-removed one\n"},
			{"new_path": "b.go", "new_file": true, "diff": "@@ -0,0 +1 @@\n+brand new\n"},
		})
	}))
	defer srv.Close()

	cr, err := newGitLabProvider(srv).GetChangeRequest(context.Background(), RepoRef{Namespace: "group", Name: "project"}, 7222)
	if err != nil {
		t.Fatalf("GetChangeRequest: %v", err)
	}
	if cr.BaseSHA != "base-sha" || cr.HeadSHA != "head-sha" {
		t.Errorf("shas = %q/%q, want base-sha/head-sha from diff_refs", cr.BaseSHA, cr.HeadSHA)
	}
	// GitLab reports no line counts on a merge request, so they are summed
	// from the diffs rather than shown as zero.
	if cr.Additions != 3 || cr.Deletions != 1 {
		t.Errorf("additions/deletions = %d/%d, want 3/1 counted from the diffs", cr.Additions, cr.Deletions)
	}
	if cr.ChangedFiles != 2 {
		t.Errorf("ChangedFiles = %d, want 2", cr.ChangedFiles)
	}
}

func TestGitLabProvider_MapsDiffsToFileStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"new_path": "added.go", "new_file": true, "diff": "@@\n+one\n"},
			{"new_path": "gone.go", "old_path": "gone.go", "deleted_file": true, "diff": "@@\n-one\n"},
			{"new_path": "new-name.go", "old_path": "old-name.go", "renamed_file": true, "diff": ""},
			{"new_path": "touched.go", "diff": "@@\n+one\n-two\n"},
		})
	}))
	defer srv.Close()

	files, err := newGitLabProvider(srv).GetChangeRequestFiles(context.Background(), RepoRef{Namespace: "owner", Name: "repo"}, 1)
	if err != nil {
		t.Fatalf("GetChangeRequestFiles: %v", err)
	}
	want := []string{FileStatusAdded, FileStatusRemoved, FileStatusRenamed, FileStatusModified}
	if len(files) != len(want) {
		t.Fatalf("files = %d, want %d", len(files), len(want))
	}
	for i, status := range want {
		if files[i].Status != status {
			t.Errorf("files[%d].Status = %q, want %q", i, files[i].Status, status)
		}
	}
	if files[3].Additions != 1 || files[3].Deletions != 1 || files[3].Changes != 2 {
		t.Errorf("modified file counts = %+v, want 1/1/2", files[3])
	}
	// The diff body is passed through untouched: it is what the UI renders.
	if files[0].Patch != "@@\n+one\n" {
		t.Errorf("Patch = %q, want the unified diff verbatim", files[0].Patch)
	}
}

func TestCountDiffLines_IgnoresFileHeaders(t *testing.T) {
	// GitLab's diff body starts at the hunk header, but a provider that ever
	// includes ---/+++ lines must not inflate the counts by one each.
	additions, deletions := countDiffLines("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n")
	if additions != 1 || deletions != 1 {
		t.Fatalf("additions/deletions = %d/%d, want 1/1", additions, deletions)
	}
}

func TestGitLabProvider_TranslatesSentinelErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{status: http.StatusNotFound, want: ErrNotFound},
		{status: http.StatusUnauthorized, want: ErrUnauthorized},
		{status: http.StatusTooManyRequests, want: ErrRateLimited},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			_, err := newGitLabProvider(srv).GetRepository(context.Background(), RepoRef{Namespace: "owner", Name: "repo"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGitLabProvider_PreservesTreeTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always another page: the client stops at its ceiling and reports it.
		w.Header().Set("x-next-page", "99")
		json.NewEncoder(w).Encode([]map[string]any{{"path": "main.go", "type": "blob"}})
	}))
	defer srv.Close()

	tree, err := newGitLabProvider(srv).GetTree(context.Background(), RepoRef{Namespace: "owner", Name: "repo"}, "main")
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	if !tree.Truncated {
		t.Fatal("Truncated was lost in translation")
	}
	if len(tree.BlobPaths()) == 0 {
		t.Fatal("BlobPaths() is empty, want the entries that were fetched")
	}
}

func TestGitLabProvider_RegisterWebhookReturnsGitLabEventNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 4711})
	}))
	defer srv.Close()

	hook, err := newGitLabProvider(srv).RegisterWebhook(context.Background(), RepoRef{Namespace: "owner", Name: "repo"}, "https://idp.example.com/hook", "secret")
	if err != nil {
		t.Fatalf("RegisterWebhook: %v", err)
	}
	if hook.ID != "4711" {
		t.Errorf("ID = %q, want 4711", hook.ID)
	}
	// The stored config should say what GitLab actually subscribed to, not
	// GitHub's `pull_request`.
	found := false
	for _, event := range hook.Events {
		if event == "merge_request" {
			found = true
		}
	}
	if !found {
		t.Errorf("events = %v, want merge_request among them", hook.Events)
	}
}

func TestGitLabProvider_CloneAuthUsesOAuth2Username(t *testing.T) {
	user, password := NewGitLabProvider("glpat-token").CloneAuth()
	if user != "oauth2" || password != "glpat-token" {
		t.Fatalf("CloneAuth() = %q/%q, want oauth2/glpat-token", user, password)
	}
}
