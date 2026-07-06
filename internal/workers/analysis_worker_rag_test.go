package workers

import (
	"context"
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

func TestAddedLines(t *testing.T) {
	patch := "@@ -1,3 +1,4 @@\n context\n-removed line\n+added one\n+added two\n+++ b/file.go"
	got := addedLines(patch)
	if !strings.Contains(got, "added one") || !strings.Contains(got, "added two") {
		t.Fatalf("expected added lines to be extracted, got %q", got)
	}
	if strings.Contains(got, "removed line") {
		t.Errorf("removed lines must not be included, got %q", got)
	}
	if strings.Contains(got, "b/file.go") {
		t.Errorf("the +++ file header must be skipped, got %q", got)
	}
}

func TestAddedLines_Caps(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("+some added content line\n")
	}
	got := addedLines(sb.String())
	if len(got) > ragAddedBudget+64 {
		t.Errorf("expected output capped around %d bytes, got %d", ragAddedBudget, len(got))
	}
}

// collectRelatedContext must degrade gracefully (return nil, never error/panic)
// whenever semantic search isn't usable — no changed files, or the org has no
// Voyage embedding provider configured.
func TestCollectRelatedContext_GracefulDegradation(t *testing.T) {
	repo := &models.Repository{ID: "r1", OrganizationID: "org1"}
	changed := []ai.ChangedFile{{Path: "internal/a.go", Patch: "+x := 1"}}

	cases := []struct {
		name string
		cfg  *models.OrganizationConfig
		chg  []ai.ChangedFile
	}{
		{name: "no changed files", cfg: &models.OrganizationConfig{VoyageAPIKey: "k", EmbeddingsProvider: "voyage"}, chg: nil},
		{name: "nil config", cfg: nil, chg: changed},
		{name: "no voyage key", cfg: &models.OrganizationConfig{EmbeddingsProvider: "voyage"}, chg: changed},
		{name: "provider not voyage", cfg: &models.OrganizationConfig{VoyageAPIKey: "k", EmbeddingsProvider: "openai"}, chg: changed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockRepo := &mockRepository{
				getConfigFunc: func(context.Context, string) (*models.OrganizationConfig, error) {
					return tc.cfg, nil
				},
			}
			worker := NewAnalysisWorker(mockRepo)
			if got := worker.collectRelatedContext(context.Background(), repo, tc.chg); got != nil {
				t.Errorf("expected nil related context, got %v", got)
			}
		})
	}
}
