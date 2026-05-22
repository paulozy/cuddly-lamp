package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodeAnalysis_MarshalJSON_InProgressIncludesZeroNumericFields(t *testing.T) {
	analysis := CodeAnalysis{
		ID:           "an-123",
		RepositoryID: "repo-1",
		Type:         AnalysisTypeCodeReview,
		Status:       AnalysisStatusPending,
	}

	out, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal CodeAnalysis: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, `"tokens_used":0`) {
		t.Errorf("expected tokens_used:0 to be present for in-progress/pending analysis; got %s", got)
	}
	if !strings.Contains(got, `"processing_ms":0`) {
		t.Errorf("expected processing_ms:0 to be present for in-progress/pending analysis; got %s", got)
	}
}

// TestAnalysisListResponse_MarshalJSON_IncludesLimitAndOffsetWhenZero guards
// the pagination envelope. Frontend Zod schema declares `limit` and `offset`
// as required numbers; if either is `omitempty`-omitted when zero, the whole
// response parse fails and the consumer (e.g. the issues page) renders a
// silent empty state.
func TestAnalysisListResponse_MarshalJSON_IncludesLimitAndOffsetWhenZero(t *testing.T) {
	resp := AnalysisListResponse{
		Total:    0,
		Analyses: nil,
		Limit:    0,
		Offset:   0,
	}

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal AnalysisListResponse: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, `"limit":0`) {
		t.Errorf("expected limit:0 to be present; got %s", got)
	}
	if !strings.Contains(got, `"offset":0`) {
		t.Errorf("expected offset:0 to be present; got %s", got)
	}
}

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
