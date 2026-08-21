package fakegitlab

// Fixture project paths. Each one exists to drive a specific behaviour the
// end-to-end suite asserts on.
const (
	// RunnerPath is a healthy project inside a nested group. The nesting is the
	// point: a two-segment path would address a different project.
	RunnerPath = "gitlab-org/nested-group/gitlab-runner"
	// HugePath serves an endless tree, so the client hits its page ceiling and
	// reports the listing as truncated.
	HugePath = "gitlab-org/huge-monorepo"
)

func intPtr(v int) *int { return &v }

// RunnerProject mirrors the shape of the public gitlab-org/gitlab-runner
// project, moved under a nested group.
//
// The tree is deliberately split across three pages with `.gitlab-ci.yml` on
// the last one, because gitlab.com's recursive listing is depth-first: a
// root-level file genuinely lands on a late page, and a client that stops at
// page one would report a project with CI as having none.
func RunnerProject() *Project {
	return &Project{
		ID:              250833,
		Path:            RunnerPath,
		Description:     "GitLab Runner is the open source project that is used to run your CI/CD jobs",
		DefaultBranch:   "main",
		Visibility:      "public",
		Topics:          []string{"golang", "hacktoberfest"},
		StarCount:       2569,
		ForksCount:      2651,
		OpenIssuesCount: nil, // gitlab.com sends null when issues are disabled
		Languages: map[string]float64{
			"Go":         98.31,
			"Shell":      0.91,
			"Makefile":   0.48,
			"HCL":        0.14, // rounds below 1: must not be dropped to zero
			"PowerShell": 0.07,
		},
		Branches: []string{"main", "0-0-stable", "0-6-stable"},
		Commits: []Commit{
			{ID: "00f99d44ee172285f44a300aafa38be7b61d77f8", Message: "Merge branch 'vtak/enable_fastzip_by_default' into 'main'", AuthorName: "Vishal Tak"},
			{ID: "a964cb19b0fd0d01489b6637ba3b1fa65f3bc9a2", Message: "fix: correct typo", AuthorName: "Dev"},
		},
		TreePages: [][]TreeEntry{
			{
				{Path: ".gitlab", Type: "tree"},
				{Path: ".gitlab/ci", Type: "tree"},
				{Path: ".gitlab/ci/build.gitlab-ci.yml", Type: "blob"},
				{Path: "commands", Type: "tree"},
				{Path: "commands/multi.go", Type: "blob"},
			},
			{
				{Path: "commands/multi_test.go", Type: "blob"},
				{Path: "helpers", Type: "tree"},
				{Path: "helpers/archives.go", Type: "blob"},
			},
			{
				{Path: "Makefile", Type: "blob"},
				{Path: "go.mod", Type: "blob"},
				{Path: "README.md", Type: "blob"},
				{Path: ".gitlab-ci.yml", Type: "blob"},
			},
		},
		MergeRequests: []MergeRequest{
			{
				ID:           522266146,
				IID:          7222,
				Title:        `fix: correct "catched" typo to "caught" in code comments`,
				Description:  "Fixes a typo in two comments.",
				State:        "opened",
				Author:       "pishel65",
				SourceBranch: "fix/doc-typo-catched-to-caught",
				TargetBranch: "main",
				SHA:          "d094172fe7d85a47a0da5d34c8ae6503a26a1205",
				BaseSHA:      "00f99d44ee172285f44a300aafa38be7b61d77f8",
				ChangesCount: "2",
			},
			{
				ID:           522266999,
				IID:          7100,
				Title:        "feat: already merged work",
				State:        "merged",
				Author:       "dev",
				SourceBranch: "feat/done",
				TargetBranch: "main",
				SHA:          "merged-sha",
				BaseSHA:      "base-sha",
				MergedAt:     "2026-08-01T10:00:00.000Z",
				ChangesCount: "1",
			},
			{
				ID:           522267001,
				IID:          7300,
				Title:        "Draft: work in progress",
				State:        "opened",
				Draft:        true,
				Author:       "dev",
				SourceBranch: "wip/thing",
				TargetBranch: "main",
				SHA:          "wip-sha",
				BaseSHA:      "00f99d44ee172285f44a300aafa38be7b61d77f8",
				ChangesCount: "1000+", // too large to count: must not become a number
			},
		},
		Diffs: map[int64][]Diff{
			7222: {
				{
					OldPath: "commands/multi.go",
					NewPath: "commands/multi.go",
					Diff:    "@@ -898,7 +898,7 @@ func (mr *RunCommand) serveMetrics(mux *http.ServeMux) {\n \t}\n-\t// Metrics about catched failures\n+\t// Metrics about caught failures\n",
				},
				{
					NewPath: "docs/NEW.md",
					NewFile: true,
					Diff:    "@@ -0,0 +1,2 @@\n+# New\n+content\n",
				},
			},
		},
	}
}

// HugeProject never runs out of tree pages, which is the only way to exercise
// the ceiling the client enforces and the Truncated flag it synthesizes.
func HugeProject() *Project {
	return &Project{
		ID:              999001,
		Path:            HugePath,
		DefaultBranch:   "main",
		Visibility:      "private",
		StarCount:       1,
		OpenIssuesCount: intPtr(4),
		Languages:       map[string]float64{"Go": 100},
		Branches:        []string{"main"},
		Commits:         []Commit{{ID: "huge-sha", Message: "init", AuthorName: "Dev"}},
		EndlessTree:     true,
	}
}

// Default returns the fixture set the end-to-end suite runs against.
func Default(token string) *Server {
	return New(token, RunnerProject(), HugeProject())
}
