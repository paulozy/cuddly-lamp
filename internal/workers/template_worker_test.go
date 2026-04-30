package workers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"gorm.io/datatypes"
)

type templateRepoMock struct {
	storage.Repository
	template       *models.CodeTemplate
	repository     *models.Repository
	updates        []models.TemplateStatus
	getConfigFunc  func(ctx context.Context, orgID string) (*models.OrganizationConfig, error)
	updateTemplate func(ctx context.Context, template *models.CodeTemplate) error
}

func (m *templateRepoMock) GetCodeTemplate(ctx context.Context, id string) (*models.CodeTemplate, error) {
	return m.template, nil
}

func (m *templateRepoMock) UpdateCodeTemplate(ctx context.Context, template *models.CodeTemplate) error {
	m.updates = append(m.updates, template.Status)
	if m.updateTemplate != nil {
		return m.updateTemplate(ctx, template)
	}
	copied := *template
	m.template = &copied
	return nil
}

func (m *templateRepoMock) GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
	if m.getConfigFunc != nil {
		return m.getConfigFunc(ctx, orgID)
	}
	return &models.OrganizationConfig{OrganizationID: orgID, AnthropicAPIKey: "anthropic-key"}, nil
}

func (m *templateRepoMock) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	return m.repository, nil
}

func TestTemplateWorkerHandleCompleted(t *testing.T) {
	repoID := "repo-1"
	mockRepo := &templateRepoMock{
		template: &models.CodeTemplate{
			ID:             "template-1",
			OrganizationID: "org-1",
			RepositoryID:   &repoID,
			Prompt:         "Create CRUD",
			Status:         models.TemplateStatusPending,
			Files:          datatypes.NewJSONType([]ai.GeneratedFile{}),
		},
		repository: &models.Repository{
			ID:             repoID,
			OrganizationID: "org-1",
			Metadata: models.RepositoryMetadata{
				Languages:  map[string]int{"Go": 100, "SQL": 20},
				Frameworks: []string{"Gin"},
				Topics:     []string{"api"},
				HasCI:      true,
				HasTests:   true,
			},
		},
	}

	worker := NewTemplateWorker(mockRepo)
	worker.generatorFactory = func(apiKey string) ai.Generator {
		if apiKey != "anthropic-key" {
			t.Fatalf("apiKey = %q, want anthropic-key", apiKey)
		}
		return &ai.MockGenerator{GenerateTemplateFunc: func(ctx context.Context, req *ai.TemplateRequest) (*ai.TemplateResult, error) {
			if req.Stack.PrimaryLanguage != "Go" {
				t.Fatalf("PrimaryLanguage = %q, want Go", req.Stack.PrimaryLanguage)
			}
			return &ai.TemplateResult{
				Summary:    "Generated API",
				Files:      []ai.GeneratedFile{{Path: "main.go", Content: "package main\n", Language: "go"}},
				Model:      "test-model",
				TokensUsed: 77,
			}, nil
		}}
	}

	task := newTemplateTask(t)
	if err := worker.Handle(context.Background(), task); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if got, want := mockRepo.updates, []models.TemplateStatus{models.TemplateStatusGenerating, models.TemplateStatusCompleted}; !sameStatuses(got, want) {
		t.Fatalf("updates = %+v, want %+v", got, want)
	}
	if mockRepo.template.Summary != "Generated API" || mockRepo.template.TokensUsed != 77 {
		t.Fatalf("template result = %+v, want generated result", mockRepo.template)
	}
}

func TestTemplateWorkerHandleFailed(t *testing.T) {
	mockRepo := &templateRepoMock{
		template: &models.CodeTemplate{
			ID:             "template-1",
			OrganizationID: "org-1",
			Prompt:         "Create CRUD",
			Status:         models.TemplateStatusPending,
			Files:          datatypes.NewJSONType([]ai.GeneratedFile{}),
		},
	}

	worker := NewTemplateWorker(mockRepo)
	worker.generatorFactory = func(apiKey string) ai.Generator {
		return &ai.MockGenerator{GenerateTemplateFunc: func(ctx context.Context, req *ai.TemplateRequest) (*ai.TemplateResult, error) {
			return nil, errors.New("provider down")
		}}
	}

	if err := worker.Handle(context.Background(), newTemplateTask(t)); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if got, want := mockRepo.updates, []models.TemplateStatus{models.TemplateStatusGenerating, models.TemplateStatusFailed}; !sameStatuses(got, want) {
		t.Fatalf("updates = %+v, want %+v", got, want)
	}
	if mockRepo.template.ErrorMessage == "" {
		t.Fatal("ErrorMessage is empty, want failure reason")
	}
}

func newTemplateTask(t *testing.T) *asynq.Task {
	t.Helper()
	payload := tasks.GenerateTemplatePayload{
		TemplateID:     "template-1",
		OrganizationID: "org-1",
		RepositoryID:   "repo-1",
		Prompt:         "Create CRUD",
		TriggeredByID:  "user-1",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return asynq.NewTask(tasks.TypeGenerateTemplate, data)
}

func sameStatuses(a, b []models.TemplateStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
