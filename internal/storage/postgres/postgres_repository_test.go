package postgres

import (
	"database/sql"
	"testing"
	"time"
)

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "auth handler", want: "auth handler"},
		{name: "percent", in: "100% coverage", want: `100\% coverage`},
		{name: "underscore", in: "user_id", want: `user\_id`},
		{name: "backslash", in: `path\to\file`, want: `path\\to\\file`},
		{name: "combined", in: `repo_%\path`, want: `repo\_\%\\path`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeLike(tt.in); got != tt.want {
				t.Fatalf("escapeLike(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// enrichedRepoToModel now sources coverage from the coverage_uploads LATERAL
// join instead of code_analyses.metrics. The join returns NULLs for a repo
// whose CI never uploaded, so the mapping must keep "never measured" distinct
// from "measured 0%" — collapsing the two would make every unconfigured repo
// look like it had 0% coverage.
func TestEnrichedRepoToModel_MapsCoverageFromUpload(t *testing.T) {
	uploadedAt := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	e := enrichedRepo{
		ID:                 "repo-1",
		Name:               "owner/repo",
		Type:               "github",
		OrganizationID:     "org-1",
		SyncStatus:         "synced",
		TestCoverage:       sql.NullFloat64{Float64: 82.5, Valid: true},
		TestedLines:        sql.NullInt64{Int64: 330, Valid: true},
		UncoveredLines:     sql.NullInt64{Int64: 70, Valid: true},
		CoverageStatus:     sql.NullString{String: "ok", Valid: true},
		CoverageUploadedAt: sql.NullTime{Time: uploadedAt, Valid: true},
	}

	repo := enrichedRepoToModel(e)

	if repo.SyncStatus != "synced" {
		t.Fatalf("SyncStatus = %q, want %q", repo.SyncStatus, "synced")
	}
	stats := repo.EnrichedStats
	if stats == nil {
		t.Fatal("EnrichedStats is nil")
	}
	if !stats.HasCoverage {
		t.Error("HasCoverage = false, want true when the join returned a row")
	}
	if stats.TestCoverage != 82.5 {
		t.Errorf("TestCoverage = %v, want 82.5", stats.TestCoverage)
	}
	if stats.TestedLines != 330 || stats.UncoveredLines != 70 {
		t.Errorf("lines = %d/%d, want 330/70", stats.TestedLines, stats.UncoveredLines)
	}
	if stats.CoverageStatus != "ok" {
		t.Errorf("CoverageStatus = %q, want %q", stats.CoverageStatus, "ok")
	}
	if stats.CoverageUploadedAt == nil || *stats.CoverageUploadedAt != uploadedAt.Format(time.RFC3339) {
		t.Errorf("CoverageUploadedAt = %v, want %v", stats.CoverageUploadedAt, uploadedAt.Format(time.RFC3339))
	}
}

// A repository with no coverage upload at all must report HasCoverage=false
// rather than a bare 0.0, so the UI can say "not configured" instead of
// showing a red 0%.
func TestEnrichedRepoToModel_NoCoverageUploadIsNotZeroPercent(t *testing.T) {
	repo := enrichedRepoToModel(enrichedRepo{
		ID:             "repo-2",
		Name:           "owner/repo",
		Type:           "github",
		OrganizationID: "org-1",
		SyncStatus:     "synced",
	})

	stats := repo.EnrichedStats
	if stats == nil {
		t.Fatal("EnrichedStats is nil")
	}
	if stats.HasCoverage {
		t.Error("HasCoverage = true, want false when no coverage upload exists")
	}
	if stats.TestCoverage != 0 {
		t.Errorf("TestCoverage = %v, want 0", stats.TestCoverage)
	}
	if stats.CoverageUploadedAt != nil {
		t.Errorf("CoverageUploadedAt = %v, want nil", stats.CoverageUploadedAt)
	}
}
