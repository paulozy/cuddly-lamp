package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"gorm.io/datatypes"
)

// orgCtxFakeRepo embeds storage.Repository (interface) and overrides only the
// methods OrgContextBuilder actually calls. Anything unimplemented panics —
// the panic surfaces test misuse instead of silently returning zero values.
type orgCtxFakeRepo struct {
	storage.Repository
	org   *models.Organization
	repos []models.Repository
	rels  []models.RepositoryRelationship
	docs  map[string][]models.DocGeneration // repoID → docs
	// latestSummary keyed by repoID → summary text; missing = no analysis.
	latestSummary map[string]string
}

func (r *orgCtxFakeRepo) GetOrganization(_ context.Context, id string) (*models.Organization, error) {
	if r.org == nil || r.org.ID != id {
		return nil, nil
	}
	return r.org, nil
}

func (r *orgCtxFakeRepo) ListRepositories(_ context.Context, _ *storage.RepositoryFilter) ([]models.Repository, int64, error) {
	out := make([]models.Repository, len(r.repos))
	copy(out, r.repos)
	return out, int64(len(out)), nil
}

func (r *orgCtxFakeRepo) ListRepositoryRelationships(_ context.Context, _ storage.RepositoryRelationshipFilter) ([]models.RepositoryRelationship, error) {
	return r.rels, nil
}

func (r *orgCtxFakeRepo) GetLatestAnalysis(_ context.Context, repoID string, _ models.AnalysisType) (*models.CodeAnalysis, error) {
	if s, ok := r.latestSummary[repoID]; ok {
		return &models.CodeAnalysis{SummaryText: s}, nil
	}
	return nil, nil
}

func (r *orgCtxFakeRepo) ListDocGenerationsForRepo(_ context.Context, repoID string) ([]models.DocGeneration, error) {
	return r.docs[repoID], nil
}

func TestOrgContextBuilder_ReturnsNilForEmptyOrg(t *testing.T) {
	fake := &orgCtxFakeRepo{
		org: &models.Organization{ID: "org-1", Name: "Acme"},
	}
	b := NewOrgContextBuilder(fake)

	snap, err := b.Build(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Fatalf("expected nil snapshot for empty org, got %+v", snap)
	}
}

func TestOrgContextBuilder_AggregatesReposGraphAndDocs(t *testing.T) {
	fake := &orgCtxFakeRepo{
		org: &models.Organization{ID: "org-1", Name: "Acme"},
		repos: []models.Repository{
			{
				ID:             "repo-web",
				Name:           "web",
				Type:           "github",
				OrganizationID: "org-1",
				Metadata: models.RepositoryMetadata{
					Languages:  map[string]int{"TypeScript": 9001, "CSS": 200},
					Frameworks: []string{"Next.js"},
				},
			},
			{
				ID:             "repo-api",
				Name:           "api",
				Type:           "github",
				OrganizationID: "org-1",
				Metadata: models.RepositoryMetadata{
					Languages:  map[string]int{"Go": 12345},
					Frameworks: []string{"Gin"},
				},
			},
		},
		rels: []models.RepositoryRelationship{
			{
				ID:                 "rel-1",
				SourceRepositoryID: "repo-web",
				TargetRepositoryID: "repo-api",
				Kind:               models.RepositoryRelationshipKindHTTP,
			},
		},
		latestSummary: map[string]string{
			"repo-api": "Mature Go service, 78% test coverage, no critical issues.",
		},
		docs: map[string][]models.DocGeneration{
			"repo-api": {
				{
					Status: models.DocGenerationStatusCompleted,
					Types:  datatypes.JSONSlice[string]{"architecture", "service_doc"},
				},
			},
		},
	}
	b := NewOrgContextBuilder(fake)

	snap, err := b.Build(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.RepositoryCount != 2 {
		t.Errorf("RepositoryCount = %d, want 2", snap.RepositoryCount)
	}
	for _, want := range []string{
		"# Organization: Acme",
		"**Repositories**: 2",
		"## Repositories",
		"| web |",
		"| api |",
		"Mature Go service",
		"## Relationships",
		"web → api (`http`)",
		"## Existing Per-Repo Documentation",
		"api: architecture, service_doc",
	} {
		if !strings.Contains(snap.Markdown, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, snap.Markdown)
		}
	}
}

func TestOrgContextBuilder_ErrorsOnMissingOrg(t *testing.T) {
	fake := &orgCtxFakeRepo{} // no org configured
	b := NewOrgContextBuilder(fake)

	_, err := b.Build(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing org")
	}
}
