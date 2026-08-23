// Package detect derives CI and test signals from a repository's file listing.
//
// Detect is a pure function over a path list: no network, no database, no
// context. The caller does the I/O and hands the paths in, which makes every
// rule testable against a literal []string and keeps the heuristics in one
// auditable place.
//
// The governing asymmetry: a wrong "this repository has no tests" is far worse
// than no answer at all. So "no" is asserted only when all three of these hold
// — the listing is complete, the repository uses a language we have confident
// patterns for, and nothing matched. Everything else is Unknown.
package detect

import (
	"sort"
	"strings"
)

type Result string

const (
	ResultYes     Result = "yes"
	ResultNo      Result = "no"
	ResultUnknown Result = "unknown"
)

// Reason discriminates *why* a result is Unknown, so the UI can say something
// more useful than "we don't know".
type Reason string

const (
	ReasonMatched         Reason = "matched"
	ReasonNoMatch         Reason = "no_match"
	ReasonTreeTruncated   Reason = "tree_truncated"
	ReasonUnsupportedLang Reason = "unsupported_language"
	ReasonNoListing       Reason = "no_listing"
)

// Detection is one signal's outcome plus the proof behind it.
type Detection struct {
	Result Result
	// Evidence names the paths that proved the signal, capped so the row stays
	// small. "No tests" without evidence is a support ticket; naming the file
	// ends the argument.
	Evidence []string
	// RuleID identifies which rule fired, so a bad result can be traced to the
	// rule that produced it. Stable string, never renamed.
	RuleID string
	Reason Reason
}

const maxEvidence = 3

// ── CI rules ─────────────────────────────────────────────────────────────────
//
// CI detection needs no vendor filter and no language gate. Vendoring is
// handled by matching root-anchored configs on the *exact* path: a
// `node_modules/lodash/.travis.yml` never matches `.travis.yml`. That single
// rule kills the whole false-positive class.

// exactCIPaths are configs that only count at the repository root.
var exactCIPaths = map[string]string{
	".gitlab-ci.yml":           "ci.gitlab",
	".gitlab-ci.yaml":          "ci.gitlab",
	".travis.yml":              "ci.travis",
	"Jenkinsfile":              "ci.jenkins",
	"jenkinsfile":              "ci.jenkins",
	"azure-pipelines.yml":      "ci.azure",
	".azure-pipelines.yml":     "ci.azure",
	"bitbucket-pipelines.yml":  "ci.bitbucket",
	"appveyor.yml":             "ci.appveyor",
	".appveyor.yml":            "ci.appveyor",
	".drone.yml":               "ci.drone",
	".drone.star":              "ci.drone",
	".cirrus.yml":              "ci.cirrus",
	"cloudbuild.yaml":          "ci.cloudbuild",
	"cloudbuild.yml":           "ci.cloudbuild",
	"buildspec.yml":            "ci.codebuild",
	"buildspec.yaml":           "ci.codebuild",
	"screwdriver.yaml":         "ci.screwdriver",
	"codefresh.yml":            "ci.codefresh",
	".woodpecker.yml":          "ci.woodpecker",
	".woodpecker.yaml":         "ci.woodpecker",
	".semaphore/semaphore.yml": "ci.semaphore",
	".circleci/config.yml":     "ci.circleci",
	".circleci/config.yaml":    "ci.circleci",
}

// prefixCIPaths are directories whose contents imply CI. A directory entry
// alone proves nothing — these match against file paths, so at least one file
// must exist inside.
var prefixCIPaths = []struct {
	Prefix string
	RuleID string
}{
	{".github/workflows/", "ci.github_actions"},
	{".buildkite/", "ci.buildkite"},
	{".tekton/", "ci.tekton"},
	{".teamcity/", "ci.teamcity"},
	{"bamboo-specs/", "ci.bamboo"},
	{".harness/", "ci.harness"},
	{".woodpecker/", "ci.woodpecker"},
	{"azure-pipelines/", "ci.azure"},
}

// DetectCI reports whether the repository has automated pipelines configured.
//
// The claim is deliberately "pipelines are configured", not "CI runs tests":
// a .github/workflows directory routinely holds only a stale-issue bot or a
// deploy job, and filenames cannot tell those apart from a build.
func DetectCI(paths []string, truncated bool) Detection {
	if len(paths) == 0 && !truncated {
		return Detection{Result: ResultUnknown, Reason: ReasonNoListing}
	}

	for _, p := range paths {
		if rule, ok := exactCIPaths[p]; ok {
			return Detection{Result: ResultYes, Evidence: []string{p}, RuleID: rule, Reason: ReasonMatched}
		}
		for _, pre := range prefixCIPaths {
			if strings.HasPrefix(p, pre.Prefix) {
				return Detection{Result: ResultYes, Evidence: []string{p}, RuleID: pre.RuleID, Reason: ReasonMatched}
			}
		}
	}

	// A truncated listing can prove presence but never absence.
	if truncated {
		return Detection{Result: ResultUnknown, Reason: ReasonTreeTruncated}
	}
	return Detection{Result: ResultNo, Reason: ReasonNoMatch}
}

// ── vendored paths ───────────────────────────────────────────────────────────
//
// Subset of github-linguist's vendor.yml. Deliberately omits linguist's
// `.github/`, `Jenkinsfile` and `testdata/` entries: the first two are our own
// CI evidence, and in Go `testdata/` is a legitimate (if weak) test signal.
var vendoredSegments = map[string]bool{
	"node_modules": true, "bower_components": true, "vendor": true, "vendors": true,
	"third_party": true, "thirdparty": true, "3rdparty": true, "externals": true,
	"Godeps": true, "dist": true, "deps": true, "Carthage": true, "Pods": true,
	".yarn": true, "site-packages": true, ".venv": true, "venv": true, "env": true,
}

// IsVendored reports whether path sits inside a vendored dependency tree.
//
// Exported because the architecture derivers need the same list. One auditable
// list, derived from github-linguist's `vendor.yml`, is the filter that kills a
// whole class of false positive — `node_modules/lodash/package.json` is not a
// manifest of this repository. Two copies of the list diverge within a year.
func IsVendored(path string) bool { return isVendored(path) }

func isVendored(path string) bool {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if vendoredSegments[seg] {
			return true
		}
		// Fixture directories are the classic false positive: a `tests/fixtures`
		// tree full of JSON is not a test suite.
		if i+1 < len(segments) && (seg == "test" || seg == "tests" || seg == "spec" || seg == "specs") &&
			segments[i+1] == "fixtures" {
			return true
		}
	}
	return false
}

// ── test rules ───────────────────────────────────────────────────────────────

type testRule struct {
	RuleID string
	Match  func(path string, segments []string) bool
}

func hasSegment(segments []string, want string) bool {
	for _, s := range segments {
		if s == want {
			return true
		}
	}
	return false
}

func hasAnySuffix(path string, suffixes ...string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

// testRulesByLanguage is gated on the repository's languages. A rule only runs
// for a language the repository actually contains, so a Cypress spec in a Go
// repo is still found (JavaScript will be in the language map) while a Java
// pattern never fires on a Python project.
var testRulesByLanguage = map[string][]testRule{
	"go": {{
		RuleID: "go.test_suffix",
		Match:  func(p string, _ []string) bool { return strings.HasSuffix(p, "_test.go") },
	}},
	"javascript": jsTestRules(),
	"typescript": jsTestRules(),
	"python": {
		{RuleID: "python.test_prefix", Match: func(p string, seg []string) bool {
			base := seg[len(seg)-1]
			return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")
		}},
		{RuleID: "python.test_suffix", Match: func(p string, _ []string) bool { return strings.HasSuffix(p, "_test.py") }},
		{RuleID: "python.conftest", Match: func(p string, seg []string) bool { return seg[len(seg)-1] == "conftest.py" }},
	},
	"java":   javaTestRules(),
	"kotlin": javaTestRules(),
	"ruby": {
		{RuleID: "ruby.rspec", Match: func(p string, _ []string) bool { return strings.HasSuffix(p, "_spec.rb") }},
		{RuleID: "ruby.minitest", Match: func(p string, _ []string) bool { return strings.HasSuffix(p, "_test.rb") }},
	},
	"php": {
		{RuleID: "php.phpunit", Match: func(p string, _ []string) bool { return strings.HasSuffix(p, "Test.php") }},
	},
}

func jsTestRules() []testRule {
	exts := []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts"}
	return []testRule{
		{RuleID: "js.test_dir", Match: func(_ string, seg []string) bool { return hasSegment(seg, "__tests__") }},
		{RuleID: "js.test_suffix", Match: func(p string, _ []string) bool {
			for _, e := range exts {
				if hasAnySuffix(p, ".test"+e, ".spec"+e) {
					return true
				}
			}
			return false
		}},
		{RuleID: "js.e2e_config", Match: func(_ string, seg []string) bool {
			base := seg[len(seg)-1]
			return strings.HasPrefix(base, "playwright.config.") ||
				strings.HasPrefix(base, "cypress.config.") ||
				strings.HasPrefix(base, "vitest.config.") ||
				strings.HasPrefix(base, "jest.config.")
		}},
	}
}

func javaTestRules() []testRule {
	return []testRule{
		{RuleID: "java.maven_test_tree", Match: func(p string, _ []string) bool {
			return strings.Contains(p, "src/test/java/") || strings.Contains(p, "src/test/kotlin/") ||
				strings.Contains(p, "src/test/groovy/") || strings.Contains(p, "src/androidTest/")
		}},
		{RuleID: "java.test_class", Match: func(p string, _ []string) bool {
			return hasAnySuffix(p, "Test.java", "Tests.java", "IT.java", "Test.kt", "Tests.kt")
		}},
	}
}

// confidentLanguages are the languages whose conventions we trust enough to
// assert an absence. Rust is deliberately excluded: idiomatic Rust unit tests
// live inline behind `#[cfg(test)]`, so a well-tested crate can contain zero
// test files. C, C++, Shell, Terraform and the like have no convention worth
// betting a red check on.
var confidentLanguages = map[string]bool{
	"go": true, "javascript": true, "typescript": true, "java": true,
	"kotlin": true, "ruby": true, "php": true, "python": true,
}

// DetectTests reports whether the repository has an automated test suite.
func DetectTests(paths []string, languages map[string]int, truncated bool) Detection {
	if len(paths) == 0 && !truncated {
		return Detection{Result: ResultUnknown, Reason: ReasonNoListing}
	}

	// Which rule sets apply. Gated on the full language set rather than the
	// primary language, so a Go service with a Cypress suite is still found.
	// Sorted so rule order — and therefore which rule gets reported as the
	// evidence — is stable across runs. Ranging a map directly would make the
	// same repository report `ruby.rspec` on one sync and `js.test_suffix` on
	// the next, which reads as churn in the UI for no reason.
	langKeys := make([]string, 0, len(languages))
	for lang := range languages {
		langKeys = append(langKeys, strings.ToLower(lang))
	}
	sort.Strings(langKeys)

	var active []testRule
	anyConfident := false
	for _, key := range langKeys {
		if rules, ok := testRulesByLanguage[key]; ok {
			active = append(active, rules...)
		}
		if confidentLanguages[key] {
			anyConfident = true
		}
	}

	evidence := make([]string, 0, maxEvidence)
	ruleID := ""
	for _, p := range paths {
		if isVendored(p) {
			continue
		}
		segments := strings.Split(p, "/")
		for _, rule := range active {
			if rule.Match(p, segments) {
				if ruleID == "" {
					ruleID = rule.RuleID
				}
				if len(evidence) < maxEvidence {
					evidence = append(evidence, p)
				}
				break
			}
		}
		if len(evidence) == maxEvidence {
			break
		}
	}

	if len(evidence) > 0 {
		return Detection{Result: ResultYes, Evidence: evidence, RuleID: ruleID, Reason: ReasonMatched}
	}

	// Absence is only assertable when the listing was complete *and* we know the
	// conventions of a language the repository actually uses.
	if truncated {
		return Detection{Result: ResultUnknown, Reason: ReasonTreeTruncated}
	}
	if !anyConfident {
		return Detection{Result: ResultUnknown, Reason: ReasonUnsupportedLang}
	}
	return Detection{Result: ResultNo, Reason: ReasonNoMatch}
}
