package models

import "testing"

func TestGetQualityScore_NotConfigured_SkipsCoverageDeduction(t *testing.T) {
	withStatus := CodeAnalysis{
		IssueCount: 0,
		Metrics: CodeMetrics{
			TestCoverage:   0,
			CoverageStatus: "not_configured",
		},
	}
	if got := withStatus.GetQualityScore(); got != 100 {
		t.Fatalf("not_configured score = %d, want 100 (no deduction)", got)
	}

	measured := CodeAnalysis{
		IssueCount: 0,
		Metrics: CodeMetrics{
			TestCoverage:   0,
			CoverageStatus: "ok",
		},
	}
	if got := measured.GetQualityScore(); got >= 100 {
		t.Fatalf("measured 0%% score = %d, want < 100 (full deduction)", got)
	}
}

func TestGetQualityScore_Failed_AlsoSkipsDeduction(t *testing.T) {
	failed := CodeAnalysis{
		IssueCount: 0,
		Metrics: CodeMetrics{
			TestCoverage:   0,
			CoverageStatus: "failed",
		},
	}
	if got := failed.GetQualityScore(); got != 100 {
		t.Fatalf("failed score = %d, want 100 (no deduction)", got)
	}
}

func TestGetQualityScore_Partial_AppliesDeduction(t *testing.T) {
	partial := CodeAnalysis{
		IssueCount: 0,
		Metrics: CodeMetrics{
			TestCoverage:   50,
			CoverageStatus: "partial",
		},
	}
	// 80 - 50 = 30 / 4 = 7 deduction
	if got := partial.GetQualityScore(); got != 93 {
		t.Fatalf("partial score = %d, want 93", got)
	}
}
