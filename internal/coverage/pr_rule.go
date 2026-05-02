package coverage

import (
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
)

// PRCoverageGaps inspects the PR diff and the per-file coverage map. For each
// newly added file (status == "added") that is missing from coverage or has
// zero recorded lines, it emits a `medium`-severity issue. The rule is
// deterministic — it does not call the LLM and is safe to run alongside
// analyzer findings.
//
// The rule is a no-op when fileCoverage is nil/empty: this avoids generating
// noise for repos that haven't started uploading coverage yet.
func PRCoverageGaps(prFiles []github.PRFile, fileCoverage map[string]FileCoverage) []ai.CodeIssue {
	if len(fileCoverage) == 0 || len(prFiles) == 0 {
		return nil
	}

	var issues []ai.CodeIssue
	for _, f := range prFiles {
		if f.Status != "added" {
			continue
		}
		fc, ok := fileCoverage[f.Filename]
		if ok && fc.LinesTotal > 0 {
			continue
		}
		issues = append(issues, ai.CodeIssue{
			Category:      "test_coverage",
			Severity:      "medium",
			Title:         "New file without test coverage",
			Description:   "This file was added in the pull request but has no recorded test coverage. Consider adding tests before merging.",
			Suggestion:    "Add unit or integration tests that exercise the new code paths and re-upload coverage to the IDP.",
			FilePath:      f.Filename,
			Line:          0,
			Column:        0,
			IsAIGenerated: false,
			Confidence:    1.0,
		})
	}
	return issues
}
