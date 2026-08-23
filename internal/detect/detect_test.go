package detect

import "testing"

func goRepo() map[string]int { return map[string]int{"Go": 1000} }

// ── CI ───────────────────────────────────────────────────────────────────────

func TestDetectCI_FindsEachEcosystem(t *testing.T) {
	tests := []struct{ name, path, rule string }{
		{"github actions", ".github/workflows/ci.yml", "ci.github_actions"},
		{"gitlab", ".gitlab-ci.yml", "ci.gitlab"},
		{"circleci", ".circleci/config.yml", "ci.circleci"},
		{"jenkins", "Jenkinsfile", "ci.jenkins"},
		{"travis", ".travis.yml", "ci.travis"},
		{"azure", "azure-pipelines.yml", "ci.azure"},
		{"buildkite", ".buildkite/pipeline.yml", "ci.buildkite"},
		{"tekton", ".tekton/pull-request.yaml", "ci.tekton"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectCI([]string{"main.go", tt.path}, false)
			if got.Result != ResultYes {
				t.Fatalf("result = %q, want yes", got.Result)
			}
			if got.RuleID != tt.rule {
				t.Errorf("rule = %q, want %q", got.RuleID, tt.rule)
			}
			if len(got.Evidence) != 1 || got.Evidence[0] != tt.path {
				t.Errorf("evidence = %v, want [%s]", got.Evidence, tt.path)
			}
		})
	}
}

// The single most common false positive: a dependency's own CI config, shipped
// inside the repository. Root-anchored configs match on the exact path, so this
// cannot fire.
func TestDetectCI_IgnoresVendoredConfigs(t *testing.T) {
	paths := []string{
		"main.go",
		"vendor/github.com/foo/bar/.travis.yml",
		"node_modules/lodash/.travis.yml",
		"third_party/x/.gitlab-ci.yml",
	}
	if got := DetectCI(paths, false); got.Result != ResultNo {
		t.Errorf("result = %q (rule %q, evidence %v), want no", got.Result, got.RuleID, got.Evidence)
	}
}

// A directory entry proves nothing; only a file inside it does.
func TestDetectCI_RequiresAFileNotJustADirectory(t *testing.T) {
	if got := DetectCI([]string{"README.md"}, false); got.Result != ResultNo {
		t.Errorf("result = %q, want no", got.Result)
	}
}

// A truncated listing can prove presence but never absence.
func TestDetectCI_TruncatedListingIsUnknownNotNo(t *testing.T) {
	got := DetectCI([]string{"src/a.go", "src/b.go"}, true)
	if got.Result != ResultUnknown {
		t.Fatalf("result = %q, want unknown", got.Result)
	}
	if got.Reason != ReasonTreeTruncated {
		t.Errorf("reason = %q, want tree_truncated", got.Reason)
	}
}

func TestDetectCI_TruncatedStillProvesPresence(t *testing.T) {
	if got := DetectCI([]string{".github/workflows/ci.yml"}, true); got.Result != ResultYes {
		t.Errorf("result = %q, want yes — a match in a partial tree is still a match", got.Result)
	}
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestDetectTests_PerLanguageConventions(t *testing.T) {
	tests := []struct {
		name  string
		langs map[string]int
		path  string
		rule  string
	}{
		{"go", goRepo(), "internal/svc/sync_test.go", "go.test_suffix"},
		{"jest suffix", map[string]int{"TypeScript": 1}, "src/util.test.ts", "js.test_suffix"},
		{"jest dir", map[string]int{"JavaScript": 1}, "src/__tests__/util.js", "js.test_dir"},
		{"python prefix", map[string]int{"Python": 1}, "tests/test_api.py", "python.test_prefix"},
		{"maven tree", map[string]int{"Java": 1}, "src/test/java/com/acme/FooTest.java", "java.maven_test_tree"},
		{"rspec", map[string]int{"Ruby": 1}, "spec/models/user_spec.rb", "ruby.rspec"},
		{"phpunit", map[string]int{"PHP": 1}, "tests/Unit/UserTest.php", "php.phpunit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectTests([]string{"README.md", tt.path}, tt.langs, false)
			if got.Result != ResultYes {
				t.Fatalf("result = %q, want yes", got.Result)
			}
			if got.RuleID != tt.rule {
				t.Errorf("rule = %q, want %q", got.RuleID, tt.rule)
			}
		})
	}
}

// The highest-volume false positive: dependencies ship their own tests.
func TestDetectTests_IgnoresVendoredTests(t *testing.T) {
	paths := []string{
		"main.go",
		"vendor/github.com/foo/bar/bar_test.go",
		"node_modules/lodash/lodash.test.js",
	}
	got := DetectTests(paths, map[string]int{"Go": 1, "JavaScript": 1}, false)
	if got.Result != ResultNo {
		t.Errorf("result = %q (evidence %v), want no", got.Result, got.Evidence)
	}
}

// A `tests/fixtures` tree full of JSON is not a test suite.
func TestDetectTests_IgnoresFixtureDirectories(t *testing.T) {
	paths := []string{"main.go", "tests/fixtures/payload_test.go"}
	if got := DetectTests(paths, goRepo(), false); got.Result != ResultNo {
		t.Errorf("result = %q, want no", got.Result)
	}
}

// Rust unit tests live inline behind #[cfg(test)], so a well-tested crate can
// have no test files. Asserting "no tests" there would be plain wrong.
func TestDetectTests_NeverAssertsNoForLanguagesWithoutConventions(t *testing.T) {
	for _, lang := range []string{"Rust", "C", "Shell", "HCL"} {
		t.Run(lang, func(t *testing.T) {
			got := DetectTests([]string{"src/main.rs"}, map[string]int{lang: 1}, false)
			if got.Result != ResultUnknown {
				t.Errorf("result = %q, want unknown", got.Result)
			}
			if got.Reason != ReasonUnsupportedLang {
				t.Errorf("reason = %q, want unsupported_language", got.Reason)
			}
		})
	}
}

// Language gating is on the full set, not the primary language: a Go service
// whose only tests are Cypress specs still has tests.
func TestDetectTests_GatesOnEveryLanguageNotJustThePrimary(t *testing.T) {
	paths := []string{"main.go", "e2e/checkout.spec.ts"}
	got := DetectTests(paths, map[string]int{"Go": 9000, "TypeScript": 100}, false)
	if got.Result != ResultYes {
		t.Errorf("result = %q, want yes", got.Result)
	}
}

func TestDetectTests_TruncatedListingIsUnknownNotNo(t *testing.T) {
	got := DetectTests([]string{"src/a.go"}, goRepo(), true)
	if got.Result != ResultUnknown || got.Reason != ReasonTreeTruncated {
		t.Errorf("result = %q reason = %q, want unknown/tree_truncated", got.Result, got.Reason)
	}
}

func TestDetectTests_CapsEvidence(t *testing.T) {
	paths := []string{"a_test.go", "b_test.go", "c_test.go", "d_test.go", "e_test.go"}
	got := DetectTests(paths, goRepo(), false)
	if len(got.Evidence) != maxEvidence {
		t.Errorf("evidence count = %d, want %d", len(got.Evidence), maxEvidence)
	}
}

// An empty listing means we never got one — not that the repository is empty.
func TestDetect_NoListingIsUnknown(t *testing.T) {
	if got := DetectCI(nil, false); got.Result != ResultUnknown || got.Reason != ReasonNoListing {
		t.Errorf("CI: result = %q reason = %q, want unknown/no_listing", got.Result, got.Reason)
	}
	if got := DetectTests(nil, goRepo(), false); got.Result != ResultUnknown {
		t.Errorf("tests: result = %q, want unknown", got.Result)
	}
}

// Rule order must not depend on Go's map iteration order, or the same
// repository reports different evidence on consecutive syncs.
func TestDetectTests_EvidenceIsStableAcrossRuns(t *testing.T) {
	paths := []string{"app/models/user.rb", "spec/user_spec.rb", "app/util.test.ts"}
	langs := map[string]int{"Ruby": 5000, "TypeScript": 3000, "JavaScript": 1000}

	first := DetectTests(paths, langs, false)
	for i := 0; i < 50; i++ {
		got := DetectTests(paths, langs, false)
		if got.RuleID != first.RuleID {
			t.Fatalf("rule id varied between runs: %q then %q", first.RuleID, got.RuleID)
		}
	}
}

// ── CI system from a stored evidence path ────────────────────────────────────

// The stored metadata keeps only the evidence path, not the rule id DetectCI
// computed, so reading the system back off the path is what lets an
// already-synced repository get a correct answer with no re-sync.
func TestCISystemForPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: ".github/workflows/ci.yml", want: "ci.github_actions"},
		{path: ".github/workflows/nested/release.yaml", want: "ci.github_actions"},
		{path: ".gitlab-ci.yml", want: "ci.gitlab"},
		{path: ".gitlab-ci.yaml", want: "ci.gitlab"},
		{path: ".circleci/config.yml", want: "ci.circleci"},
		{path: "Jenkinsfile", want: "ci.jenkins"},
		{path: ".travis.yml", want: "ci.travis"},
		{path: ".buildkite/pipeline.yml", want: "ci.buildkite"},
		{path: "azure-pipelines.yml", want: "ci.azure"},
		// Not a CI config: the caller must get "" and say "unknown", never guess.
		{path: "README.md", want: ""},
		{path: "", want: ""},
		{path: "src/config.yml", want: ""},
		// Root-anchored, the same rule DetectCI relies on: a vendored dependency's
		// CI config is not this repository's CI.
		{path: "node_modules/some-pkg/.travis.yml", want: ""},
		{path: "vendor/other/.gitlab-ci.yml", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := CISystemForPath(tt.path); got != tt.want {
				t.Errorf("CISystemForPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// The two must never disagree about what a given config file is, which is why
// CISystemForPath reuses DetectCI's own tables rather than a second list.
func TestCISystemForPath_AgreesWithDetectCI(t *testing.T) {
	for _, path := range []string{
		".github/workflows/ci.yml",
		".gitlab-ci.yml",
		".circleci/config.yml",
		"Jenkinsfile",
	} {
		detection := DetectCI([]string{path}, false)
		if got := CISystemForPath(path); got != detection.RuleID {
			t.Errorf("CISystemForPath(%q) = %q, but DetectCI says %q", path, got, detection.RuleID)
		}
	}
}
