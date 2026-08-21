package workers

import (
	"context"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// mockRepository stubs only the storage methods the surviving workers touch
// (sync, webhook processing, documentation generation). The embedded
// storage.Repository panics on anything else, which is the point: a worker
// reaching for an unstubbed method is a test that should fail loudly.
type mockRepository struct {
	storage.Repository
	getRepoFunc                func(ctx context.Context, id string) (*models.Repository, error)
	updateRepoFunc             func(ctx context.Context, repo *models.Repository) error
	createDocGenerationFunc    func(ctx context.Context, doc *models.DocGeneration) error
	updateDocGenerationFunc    func(ctx context.Context, doc *models.DocGeneration) error
	getDocGenerationFunc       func(ctx context.Context, id string) (*models.DocGeneration, error)
	getLatestDocGenerationFunc func(ctx context.Context, repoID string) (*models.DocGeneration, error)
	getLatestOrgDocsFunc       func(ctx context.Context, orgID string, types []string) ([]models.DocGeneration, error)
	getConfigFunc              func(ctx context.Context, orgID string) (*models.OrganizationConfig, error)
}

func (m *mockRepository) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	if m.getRepoFunc != nil {
		return m.getRepoFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockRepository) UpdateRepository(ctx context.Context, repo *models.Repository) error {
	if m.updateRepoFunc != nil {
		return m.updateRepoFunc(ctx, repo)
	}
	return nil
}

func (m *mockRepository) CreateDocGeneration(ctx context.Context, doc *models.DocGeneration) error {
	if m.createDocGenerationFunc != nil {
		return m.createDocGenerationFunc(ctx, doc)
	}
	return nil
}

func (m *mockRepository) UpdateDocGeneration(ctx context.Context, doc *models.DocGeneration) error {
	if m.updateDocGenerationFunc != nil {
		return m.updateDocGenerationFunc(ctx, doc)
	}
	return nil
}

func (m *mockRepository) GetDocGeneration(ctx context.Context, id string) (*models.DocGeneration, error) {
	if m.getDocGenerationFunc != nil {
		return m.getDocGenerationFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockRepository) GetLatestDocGenerationForRepo(ctx context.Context, repoID string) (*models.DocGeneration, error) {
	if m.getLatestDocGenerationFunc != nil {
		return m.getLatestDocGenerationFunc(ctx, repoID)
	}
	return nil, nil
}

func (m *mockRepository) GetLatestOrgDocs(ctx context.Context, orgID string, types []string) ([]models.DocGeneration, error) {
	if m.getLatestOrgDocsFunc != nil {
		return m.getLatestOrgDocsFunc(ctx, orgID, types)
	}
	return nil, nil
}

func (m *mockRepository) GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx, orgID)
	}
	return nil, nil
}
