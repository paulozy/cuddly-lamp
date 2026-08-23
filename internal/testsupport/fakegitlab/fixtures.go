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
	// CheckoutPath and SharedPath are the pair architecture derivation needs:
	// checkout's go.mod requires the module shared's go.mod publishes, so the
	// package index has exactly one internal edge to find. Nothing else in the
	// two trees matches a manifest, which keeps the assertion unambiguous.
	CheckoutPath = "e2e-org/checkout"
	SharedPath   = "e2e-org/shared"
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
		// The issue list and contributor list the browse endpoints read.
		// "Vishal Tak" also authors a commit above, so the derived
		// last-commit lookup has a name to match on — GitLab reports no
		// username here, which is exactly the case worth exercising.
		Issues: []Issue{
			{ID: 9001, IID: 88, Title: "Sidebar does not collapse on mobile", State: "opened",
				Labels: []string{"bug", "ui"}, UserNotesCount: 3, AuthorUsername: "julia.r"},
			{ID: 9002, IID: 84, Title: "Add an empty state to the graph", State: "opened",
				Labels: []string{"enhancement"}, UserNotesCount: 1, AuthorUsername: "ana.m"},
		},
		Contributors: []Contributor{
			{Name: "Vishal Tak", Email: "vishal@example.com", Commits: 240},
			{Name: "Dev", Email: "dev@example.com", Commits: 58},
		},
		// File contents the raw endpoint serves. `.gitlab/ci/build.gitlab-ci.yml`
		// is here because it is nested: reading it proves the client encoded the
		// slashes into a single path segment instead of sending them raw.
		Files: map[string]string{
			"go.mod":                         "module gitlab.com/gitlab-org/gitlab-runner\n\ngo 1.23\n",
			".gitlab/ci/build.gitlab-ci.yml": "stages:\n  - build\n",
		},
		FilesByRef: map[string]map[string]string{
			"0-6-stable": {"go.mod": "module gitlab.com/gitlab-org/gitlab-runner\n\ngo 1.19\n"},
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
	return New(token, RunnerProject(), HugeProject(), CheckoutProject(), SharedProject())
}

// CheckoutProject declares a dependency on the module SharedProject publishes.
// Its `go.mod` is the only manifest in the tree, so the derived edge can only
// have come from it.
func CheckoutProject() *Project {
	return &Project{
		ID:              700001,
		Path:            CheckoutPath,
		Description:     "checkout service",
		DefaultBranch:   "main",
		Visibility:      "private",
		OpenIssuesCount: intPtr(0),
		Languages:       map[string]float64{"Go": 100},
		Branches:        []string{"main"},
		Commits:         []Commit{{ID: "checkout-sha", Message: "init", AuthorName: "Dev"}},
		TreePages: [][]TreeEntry{{
			{Path: "go.mod", Type: "blob"},
			{Path: "main.go", Type: "blob"},
			{Path: "openapi.yaml", Type: "blob"},
			{Path: "docker-compose.yml", Type: "blob"},
			{Path: "k8s", Type: "tree"},
			{Path: "k8s/deployment.yaml", Type: "blob"},
		}},
		Files: map[string]string{
			"go.mod": "module gitlab.com/e2e-org/checkout\n\ngo 1.25\n\nrequire gitlab.com/e2e-org/shared v1.2.0\n",
			// A real entry document: the root `openapi` field plus a REQUIRED Info
			// Object is what makes the sniff decisive.
			"openapi.yaml": "openapi: 3.0.3\ninfo:\n  title: Checkout API\n  version: 1.4.0\npaths:\n  /checkout:\n    post: {}\n  /checkout/{id}:\n    get: {}\n",
			// A compose engine: proves the engine, says nothing about the instance,
			// so the resource must come out scoped to this repository.
			"docker-compose.yml": "services:\n  api:\n    build: .\n    depends_on:\n      - db\n  db:\n    image: postgres:16\n",
			// The consumed host is a Service name SharedProject declares, which is
			// the only reason the consumption edge is allowed to exist.
			"k8s/deployment.yaml": "kind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          env:\n            - name: SHARED_API_URL\n              value: http://shared-api:8080\n",
		},
	}
}

// SharedProject publishes the module CheckoutProject requires.
func SharedProject() *Project {
	return &Project{
		ID:              700002,
		Path:            SharedPath,
		Description:     "shared library",
		DefaultBranch:   "main",
		Visibility:      "private",
		OpenIssuesCount: intPtr(0),
		Languages:       map[string]float64{"Go": 100},
		Branches:        []string{"main"},
		Commits:         []Commit{{ID: "shared-sha", Message: "init", AuthorName: "Dev"}},
		TreePages: [][]TreeEntry{{
			{Path: "go.mod", Type: "blob"},
			{Path: "shared.go", Type: "blob"},
			{Path: "k8s", Type: "tree"},
			{Path: "k8s/service.yaml", Type: "blob"},
		}},
		Files: map[string]string{
			"go.mod": "module gitlab.com/e2e-org/shared\n\ngo 1.25\n",
			// The Service declaration checkout's env value matches. In-cluster DNS is
			// a naming contract, so this is what turns the match into evidence.
			"k8s/service.yaml": "apiVersion: v1\nkind: Service\nmetadata:\n  name: shared-api\nspec:\n  ports:\n    - port: 8080\n",
		},
	}
}
