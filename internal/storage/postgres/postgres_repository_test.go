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

// Regression for the CHECK constraint violation: if the enriched SELECT does
// not return embeddings columns, enrichedRepoToModel loads the Go zero-value
// ("") and a subsequent db.Save() writes '' over the column, violating
// repositories_embeddings_status_check (migration 021). This test pins the
// mapping from scanned columns to the model so any future SELECT/struct drift
// surfaces here instead of in production UPDATEs.
func TestEnrichedRepoToModel_PropagatesEmbeddingsColumns(t *testing.T) {
	indexedAt := time.Date(2026, 5, 18, 21, 0, 0, 0, time.UTC)
	e := enrichedRepo{
		ID:                  "repo-1",
		Name:                "owner/repo",
		Type:                "github",
		OrganizationID:      "org-1",
		SyncStatus:          "synced",
		AnalysisStatus:      "completed",
		EmbeddingsStatus:    "indexed",
		EmbeddingsCount:     42,
		EmbeddingsIndexedAt: &indexedAt,
		EmbeddingsError:     sql.NullString{String: "previous failure", Valid: true},
	}

	repo := enrichedRepoToModel(e)

	if repo.EmbeddingsStatus != "indexed" {
		t.Fatalf("EmbeddingsStatus = %q, want %q", repo.EmbeddingsStatus, "indexed")
	}
	if repo.EmbeddingsCount != 42 {
		t.Fatalf("EmbeddingsCount = %d, want 42", repo.EmbeddingsCount)
	}
	if repo.EmbeddingsIndexedAt == nil || !repo.EmbeddingsIndexedAt.Equal(indexedAt) {
		t.Fatalf("EmbeddingsIndexedAt = %v, want %v", repo.EmbeddingsIndexedAt, indexedAt)
	}
	if repo.EmbeddingsError != "previous failure" {
		t.Fatalf("EmbeddingsError = %q, want %q", repo.EmbeddingsError, "previous failure")
	}
	if repo.SyncStatus != "synced" {
		t.Fatalf("SyncStatus = %q, want %q", repo.SyncStatus, "synced")
	}
	if repo.AnalysisStatus != "completed" {
		t.Fatalf("AnalysisStatus = %q, want %q", repo.AnalysisStatus, "completed")
	}
}
