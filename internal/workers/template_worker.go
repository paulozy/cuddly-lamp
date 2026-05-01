package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	anthropicclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/anthropic"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

type TemplateWorker struct {
	repo             storage.Repository
	generatorFactory func(apiKey string) ai.Generator
}

func NewTemplateWorker(repo storage.Repository) *TemplateWorker {
	return &TemplateWorker{
		repo:             repo,
		generatorFactory: func(apiKey string) ai.Generator { return anthropicclient.NewClient(apiKey) },
	}
}

func (w *TemplateWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.GenerateTemplatePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("template worker: unmarshal payload: %w", err)
	}
	if payload.TemplateID == "" {
		return fmt.Errorf("template worker: empty template_id")
	}

	template, err := w.repo.GetCodeTemplate(ctx, payload.TemplateID)
	if err != nil {
		return fmt.Errorf("template worker: get template: %w", err)
	}
	if template == nil {
		return fmt.Errorf("template worker: template not found: %s", payload.TemplateID)
	}

	cfg, err := w.repo.GetOrganizationConfig(ctx, template.OrganizationID)
	if err != nil {
		return w.failTemplate(ctx, template, fmt.Sprintf("get organization config: %v", err))
	}
	if cfg == nil || cfg.AnthropicAPIKey == "" {
		return w.failTemplate(ctx, template, "anthropic api key is not configured for organization")
	}

	template.Status = models.TemplateStatusGenerating
	template.ErrorMessage = ""
	if err := w.repo.UpdateCodeTemplate(ctx, template); err != nil {
		return fmt.Errorf("template worker: set generating: %w", err)
	}

	stack := ai.StackProfile{}
	if template.RepositoryID != nil && *template.RepositoryID != "" {
		repository, err := w.repo.GetRepository(ctx, *template.RepositoryID)
		if err != nil {
			return w.failTemplate(ctx, template, fmt.Sprintf("get repository: %v", err))
		}
		if repository == nil || repository.OrganizationID != template.OrganizationID {
			return w.failTemplate(ctx, template, "repository not found for template organization")
		}
		stack = stackProfileFromRepository(repository)
	}
	template.StackSnapshot = datatypes.NewJSONType(stack)

	generator := w.generatorFactory(cfg.AnthropicAPIKey)
	started := time.Now()
	result, err := generator.GenerateTemplate(ctx, &ai.TemplateRequest{
		Prompt:         template.Prompt,
		OrganizationID: template.OrganizationID,
		RepositoryID:   payload.RepositoryID,
		Stack:          stack,
		StackHint:      template.StackHint,
		TemplateID:     template.ID,
		OutputLanguage: cfg.ResolvedOutputLanguage(),
	})
	processingMs := time.Since(started).Milliseconds()
	if err != nil {
		utils.Error("template worker: generator failed", "template_id", template.ID, "error", err)
		return w.failTemplate(ctx, template, fmt.Sprintf("generator error: %v", err))
	}

	template.Status = models.TemplateStatusCompleted
	template.Summary = result.Summary
	template.Files = datatypes.NewJSONType(result.Files)
	template.AIModel = result.Model
	template.TokensUsed = result.TokensUsed
	template.ProcessingMs = processingMs
	template.ErrorMessage = ""
	if err := w.repo.UpdateCodeTemplate(ctx, template); err != nil {
		return fmt.Errorf("template worker: complete template: %w", err)
	}

	utils.Info("template worker: completed", "template_id", template.ID, "files", len(result.Files))
	return nil
}

func (w *TemplateWorker) failTemplate(ctx context.Context, template *models.CodeTemplate, message string) error {
	template.Status = models.TemplateStatusFailed
	template.ErrorMessage = message
	if err := w.repo.UpdateCodeTemplate(ctx, template); err != nil {
		return fmt.Errorf("template worker: mark failed: %w", err)
	}
	return nil
}

func stackProfileFromRepository(repository *models.Repository) ai.StackProfile {
	type languageSize struct {
		name  string
		lines int
	}
	languages := make([]languageSize, 0, len(repository.Metadata.Languages))
	for name, lines := range repository.Metadata.Languages {
		languages = append(languages, languageSize{name: name, lines: lines})
	}
	sort.Slice(languages, func(i, j int) bool {
		if languages[i].lines == languages[j].lines {
			return languages[i].name < languages[j].name
		}
		return languages[i].lines > languages[j].lines
	})

	profile := ai.StackProfile{
		Frameworks: append([]string(nil), repository.Metadata.Frameworks...),
		Topics:     append([]string(nil), repository.Metadata.Topics...),
		HasCI:      repository.Metadata.HasCI,
		HasTests:   repository.Metadata.HasTests,
	}
	if len(languages) > 0 {
		profile.PrimaryLanguage = languages[0].name
		for _, lang := range languages[1:] {
			profile.SecondaryLanguages = append(profile.SecondaryLanguages, lang.name)
		}
	}
	return profile
}
