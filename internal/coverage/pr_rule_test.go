package coverage

import (
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

func TestPRCoverageGaps_FlagsAddedWithoutCoverage(t *testing.T) {
	prFiles := []github.PRFile{
		{Filename: "src/new.go", Status: "added"},
		{Filename: "src/other.go", Status: "modified"},
	}
	cov := map[string]FileCoverage{
		"src/other.go": {Path: "src/other.go", LinesCovered: 5, LinesTotal: 5},
	}

	issues := PRCoverageGaps(prFiles, cov)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].FilePath != "src/new.go" {
		t.Fatalf("issue file = %q, want src/new.go", issues[0].FilePath)
	}
	if issues[0].Category != "test_coverage" || issues[0].Severity != "medium" {
		t.Fatalf("category/severity wrong: %+v", issues[0])
	}
	if issues[0].IsAIGenerated || issues[0].Confidence != 1.0 {
		t.Fatalf("issue should be deterministic with confidence 1.0: %+v", issues[0])
	}
}

func TestPRCoverageGaps_FlagsAddedWithZeroCoverage(t *testing.T) {
	prFiles := []github.PRFile{{Filename: "src/empty.go", Status: "added"}}
	cov := map[string]FileCoverage{
		"src/empty.go": {Path: "src/empty.go", LinesCovered: 0, LinesTotal: 0},
	}
	issues := PRCoverageGaps(prFiles, cov)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (zero LinesTotal counts as missing), got %d", len(issues))
	}
}

func TestPRCoverageGaps_IgnoresAddedWithCoverage(t *testing.T) {
	prFiles := []github.PRFile{{Filename: "src/new.go", Status: "added"}}
	cov := map[string]FileCoverage{
		"src/new.go": {Path: "src/new.go", LinesCovered: 0, LinesTotal: 10},
	}
	issues := PRCoverageGaps(prFiles, cov)
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d: %+v", len(issues), issues)
	}
}

func TestPRCoverageGaps_IgnoresModifiedAndOtherStatuses(t *testing.T) {
	prFiles := []github.PRFile{
		{Filename: "a", Status: "modified"},
		{Filename: "b", Status: "renamed"},
		{Filename: "c", Status: "removed"},
		{Filename: "d", Status: "copied"},
	}
	cov := map[string]FileCoverage{"x": {LinesTotal: 1}} // non-empty so guard doesn't short-circuit
	if issues := PRCoverageGaps(prFiles, cov); len(issues) != 0 {
		t.Fatalf("non-added statuses should never flag, got %d", len(issues))
	}
}

func TestPRCoverageGaps_EmptyCoverageMapShortCircuits(t *testing.T) {
	prFiles := []github.PRFile{{Filename: "new.go", Status: "added"}}
	if issues := PRCoverageGaps(prFiles, nil); issues != nil {
		t.Fatal("nil coverage should return nil to avoid noise")
	}
	if issues := PRCoverageGaps(prFiles, map[string]FileCoverage{}); issues != nil {
		t.Fatal("empty coverage should return nil to avoid noise")
	}
}
