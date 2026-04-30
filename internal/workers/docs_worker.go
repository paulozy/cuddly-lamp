package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/ai"
	anthropicclient "github.com/paulozy/idp-with-ai-backend/internal/integrations/anthropic"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

type DocsWorker struct {
	repo             storage.Repository
	generatorFactory func(apiKey string) ai.DocumentationGenerator
	githubFactory    func(token string) github.ClientInterface
	cloneRepo        func(ctx context.Context, repoURL, githubToken, branch string) (string, func(), error)
}

func NewDocsWorker(repo storage.Repository) *DocsWorker {
	return &DocsWorker{
		repo:             repo,
		generatorFactory: func(apiKey string) ai.DocumentationGenerator { return anthropicclient.NewClient(apiKey) },
		githubFactory:    func(token string) github.ClientInterface { return github.NewClient(token) },
		cloneRepo:        cloneRepositoryForDocs,
	}
}

func (w *DocsWorker) Handle(ctx context.Context, task *asynq.Task) error {
	var payload tasks.GenerateDocsPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("docs worker: unmarshal payload: %w", err)
	}
	if payload.RepositoryID == "" {
		return fmt.Errorf("docs worker: empty repository_id")
	}

	doc, err := w.loadOrCreateDocGeneration(ctx, payload)
	if err != nil {
		return err
	}

	repository, err := w.repo.GetRepository(ctx, payload.RepositoryID)
	if err != nil {
		return w.failDocGeneration(ctx, doc, fmt.Sprintf("get repository: %v", err))
	}
	if repository == nil {
		return w.failDocGeneration(ctx, doc, "repository not found")
	}

	cfg, err := w.repo.GetOrganizationConfig(ctx, repository.OrganizationID)
	if err != nil {
		return w.failDocGeneration(ctx, doc, fmt.Sprintf("get organization config: %v", err))
	}
	if cfg == nil || cfg.AnthropicAPIKey == "" {
		return w.failDocGeneration(ctx, doc, "anthropic api key is not configured for organization")
	}
	if cfg.GithubToken == "" {
		return w.failDocGeneration(ctx, doc, "github token is not configured for organization")
	}

	branch := payload.Branch
	if branch == "" {
		branch = repository.Metadata.DefaultBranch
	}
	if branch == "" {
		branch = "main"
	}

	doc.Status = models.DocGenerationStatusInProgress
	doc.Branch = branch
	doc.ErrorMessage = ""
	if err := w.repo.UpdateDocGeneration(ctx, doc); err != nil {
		return fmt.Errorf("docs worker: mark in progress: %w", err)
	}

	ownerRepo, _, err := utils.ParseRepositoryURL(repository.URL)
	if err != nil {
		return w.failDocGeneration(ctx, doc, fmt.Sprintf("invalid repository URL: %v", err))
	}
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		return w.failDocGeneration(ctx, doc, "invalid repository format")
	}
	owner, repoName := parts[0], parts[1]

	cloneDir, cleanup, err := w.cloneRepo(ctx, repository.URL, cfg.GithubToken, branch)
	if err != nil {
		return w.failDocGeneration(ctx, doc, fmt.Sprintf("clone repository: %v", err))
	}
	defer cleanup()

	ghClient := w.githubFactory(cfg.GithubToken)
	contextMarkdown := w.buildDocContext(ctx, ghClient, repository, owner, repoName, branch, cloneDir)
	generator := w.generatorFactory(cfg.AnthropicAPIKey)

	content := make(map[string]string)
	tokensUsed := 0
	for _, rawType := range doc.Types {
		docType := ai.DocumentationType(rawType)
		result, err := generator.GenerateDocumentation(ctx, &ai.DocumentationRequest{
			Type:            docType,
			RepositoryID:    repository.ID,
			RepoName:        repository.Name,
			Branch:          branch,
			Languages:       sortedLanguageNames(repository.Metadata.Languages),
			Frameworks:      append([]string(nil), repository.Metadata.Frameworks...),
			Topics:          append([]string(nil), repository.Metadata.Topics...),
			ContextMarkdown: contextMarkdown,
		})
		if err != nil {
			return w.failDocGeneration(ctx, doc, fmt.Sprintf("generate %s documentation: %v", rawType, err))
		}
		content[rawType] = result.Content
		tokensUsed += result.TokensUsed
	}

	genBranch := fmt.Sprintf("docs/auto-generated-%d", time.Now().UTC().Unix())
	if err := ghClient.CreateBranch(ctx, owner, repoName, branch, genBranch); err != nil {
		return w.failDocGeneration(ctx, doc, fmt.Sprintf("create documentation branch: %v", err))
	}

	for docType, markdown := range content {
		path, ok := docPath(docType)
		if !ok {
			continue
		}
		msg := fmt.Sprintf("docs: update %s documentation", docType)
		if err := ghClient.CreateOrUpdateFile(ctx, owner, repoName, genBranch, path, msg, markdown); err != nil {
			return w.failDocGeneration(ctx, doc, fmt.Sprintf("commit %s: %v", path, err))
		}
	}

	pr, err := ghClient.CreatePullRequest(ctx, owner, repoName, "docs: auto-generate project documentation", genBranch, branch, "Generated project documentation from repository analysis.")
	if err != nil {
		return w.failDocGeneration(ctx, doc, fmt.Sprintf("create pull request: %v", err))
	}

	doc.Status = models.DocGenerationStatusCompleted
	doc.GenBranch = genBranch
	doc.Content = datatypes.NewJSONType(content)
	doc.TokensUsed = tokensUsed
	if pr != nil {
		doc.PullRequestNumber = int(pr.Number)
		doc.PullRequestURL = pr.HTMLURL
	}
	doc.ErrorMessage = ""
	if err := w.repo.UpdateDocGeneration(ctx, doc); err != nil {
		return fmt.Errorf("docs worker: mark completed: %w", err)
	}

	utils.Info("docs worker: completed", "doc_generation_id", doc.ID, "repo_id", repository.ID, "pr", doc.PullRequestURL)
	return nil
}

func (w *DocsWorker) loadOrCreateDocGeneration(ctx context.Context, payload tasks.GenerateDocsPayload) (*models.DocGeneration, error) {
	if payload.DocGenerationID != "" {
		doc, err := w.repo.GetDocGeneration(ctx, payload.DocGenerationID)
		if err != nil {
			return nil, fmt.Errorf("docs worker: get doc generation: %w", err)
		}
		if doc == nil {
			return nil, fmt.Errorf("docs worker: doc generation not found: %s", payload.DocGenerationID)
		}
		return doc, nil
	}
	doc := &models.DocGeneration{
		RepositoryID:      payload.RepositoryID,
		Status:            models.DocGenerationStatusPending,
		Types:             datatypes.JSONSlice[string](payload.Types),
		Branch:            payload.Branch,
		TriggeredByUserID: payload.TriggeredByID,
		Content:           datatypes.NewJSONType(map[string]string{}),
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := w.repo.CreateDocGeneration(ctx, doc); err != nil {
		return nil, fmt.Errorf("docs worker: create doc generation: %w", err)
	}
	return doc, nil
}

func (w *DocsWorker) failDocGeneration(ctx context.Context, doc *models.DocGeneration, message string) error {
	utils.Error("docs worker: failing", "doc_generation_id", doc.ID, "error", message)
	doc.Status = models.DocGenerationStatusFailed
	doc.ErrorMessage = message
	if err := w.repo.UpdateDocGeneration(ctx, doc); err != nil {
		return fmt.Errorf("docs worker: mark failed: %w", err)
	}
	return fmt.Errorf("docs worker: %s", message)
}

func (w *DocsWorker) buildDocContext(ctx context.Context, ghClient github.ClientInterface, repository *models.Repository, owner, repoName, branch, cloneDir string) string {
	sb := strings.Builder{}
	sb.WriteString("## Directory Tree\n")
	sb.WriteString(renderDirectoryTree(cloneDir, 2))

	sb.WriteString("\n## Key Files\n")
	for _, rel := range []string{".env.example", "Makefile", "docker-compose.yml", "go.mod", "package.json", "README.md"} {
		if content, ok := readSmallTextFile(cloneDir, rel, 12000); ok {
			sb.WriteString(fmt.Sprintf("\n### %s\n```text\n%s\n```\n", rel, content))
		}
	}

	if commits, err := ghClient.GetCommits(ctx, owner, repoName, branch, 20); err == nil && len(commits) > 0 {
		sb.WriteString("\n## Recent Commits\n")
		for _, commit := range commits {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", shortSHA(commit.SHA), strings.Split(commit.Commit.Message, "\n")[0]))
		}
	}

	if prs, err := ghClient.ListPullRequests(ctx, owner, repoName); err == nil && len(prs) > 0 {
		sb.WriteString("\n## Recent Pull Requests\n")
		for i, pr := range prs {
			if i >= 5 {
				break
			}
			sb.WriteString(fmt.Sprintf("- #%d %s by %s\n", pr.Number, pr.Title, pr.User.Login))
		}
	}

	if analysis, err := w.repo.GetLatestAnalysis(ctx, repository.ID, models.AnalysisTypeCodeReview); err == nil && analysis != nil && analysis.SummaryText != "" {
		sb.WriteString("\n## Latest AI Analysis Summary\n")
		sb.WriteString(analysis.SummaryText)
		sb.WriteString("\n")
	}

	return truncateString(sb.String(), 60000)
}

func cloneRepositoryForDocs(ctx context.Context, repoURL, githubToken, branch string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "docs-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	opts := &git.CloneOptions{URL: repoURL, Depth: 1}
	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}
	if githubToken != "" {
		opts.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: githubToken}
	}
	if _, err := git.PlainCloneContext(ctx, dir, false, opts); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

func renderDirectoryTree(root string, maxDepth int) string {
	var lines []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(rel, ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > maxDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		prefix := strings.Repeat("  ", depth-1)
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, prefix+name)
		return nil
	})
	sort.Strings(lines)
	if len(lines) == 0 {
		return "(no files found)\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func readSmallTextFile(root, rel string, maxBytes int) (string, bool) {
	path := filepath.Join(root, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if len(data) > maxBytes {
		data = data[:maxBytes]
	}
	return strings.TrimSpace(string(data)), true
}

func docPath(docType string) (string, bool) {
	switch ai.DocumentationType(docType) {
	case ai.DocumentationTypeADR:
		return "docs/adr/README.md", true
	case ai.DocumentationTypeArchitecture:
		return "docs/ARCHITECTURE.md", true
	case ai.DocumentationTypeServiceDoc:
		return "docs/SERVICE.md", true
	case ai.DocumentationTypeGuidelines:
		return "CONTRIBUTING.md", true
	default:
		return "", false
	}
}

func sortedLanguageNames(languages map[string]int) []string {
	out := make([]string, 0, len(languages))
	for lang := range languages {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func truncateString(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n\n[truncated]\n"
}
