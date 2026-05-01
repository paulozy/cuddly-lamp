package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/github"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/datatypes"
)

type pullRequestHandlerRepo struct {
	storage.Repository
	repo              *models.Repository
	config            *models.OrganizationConfig
	latest            *models.CodeAnalysis
	latestByPR        map[int]models.CodeAnalysis
	tokensUsed        int64
	lastAnalysisRepo  string
	lastAnalysisPR    int
	lastAnalysisBatch []int
}

func (r *pullRequestHandlerRepo) GetRepository(ctx context.Context, id string) (*models.Repository, error) {
	return r.repo, nil
}

func (r *pullRequestHandlerRepo) GetOrganizationConfig(ctx context.Context, orgID string) (*models.OrganizationConfig, error) {
	return r.config, nil
}

func (r *pullRequestHandlerRepo) GetLatestAnalysisForPullRequest(ctx context.Context, repoID string, pullRequestID int, analysisType models.AnalysisType) (*models.CodeAnalysis, error) {
	r.lastAnalysisRepo = repoID
	r.lastAnalysisPR = pullRequestID
	return r.latest, nil
}

func (r *pullRequestHandlerRepo) ListLatestAnalysesForPullRequests(ctx context.Context, repoID string, pullRequestIDs []int, analysisType models.AnalysisType) (map[int]models.CodeAnalysis, error) {
	r.lastAnalysisRepo = repoID
	r.lastAnalysisBatch = append([]int(nil), pullRequestIDs...)
	return r.latestByPR, nil
}

func (r *pullRequestHandlerRepo) SumTokensUsedSince(ctx context.Context, organizationID string, since time.Time) (int64, error) {
	return r.tokensUsed, nil
}

type pullRequestHandlerEnqueuer struct {
	err      error
	taskType string
	payload  any
	enqueued bool
}

func (e *pullRequestHandlerEnqueuer) Enqueue(ctx context.Context, taskType string, payload any, opts ...asynq.Option) error {
	if e.err != nil {
		return e.err
	}
	e.taskType = taskType
	e.payload = payload
	e.enqueued = true
	return nil
}

func (e *pullRequestHandlerEnqueuer) EnqueueIn(ctx context.Context, taskType string, payload any, delay time.Duration, opts ...asynq.Option) error {
	return e.Enqueue(ctx, taskType, payload, opts...)
}

type pullRequestMockGitHub struct {
	github.ClientInterface
	pr          *github.PullRequest
	prs         []github.PullRequest
	files       []github.PRFile
	reviewID    int64
	reviewEvent string
	reviewBody  string
	comments    []github.ReviewCommentInput
}

func (m *pullRequestMockGitHub) ListPullRequests(ctx context.Context, owner, repo string) ([]github.PullRequest, error) {
	return m.prs, nil
}

func (m *pullRequestMockGitHub) GetPullRequest(ctx context.Context, owner, repo string, prID int64) (*github.PullRequest, error) {
	return m.pr, nil
}

func (m *pullRequestMockGitHub) GetPullRequestFiles(ctx context.Context, owner, repo string, prID int64) ([]github.PRFile, error) {
	return m.files, nil
}

func (m *pullRequestMockGitHub) CreatePullRequestReview(ctx context.Context, owner, repo string, prID int64, body string, event string, comments []github.ReviewCommentInput) (int64, error) {
	m.reviewBody = body
	m.reviewEvent = event
	m.comments = comments
	return m.reviewID, nil
}

func newPullRequestHandlerTestContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), utils.ContextKeyOrganization, "org-1"))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "repo-1"}, {Key: "pr_number", Value: "42"}}
	return c, w
}

func TestAnalysisHandler_ListPullRequests_AttachesLatestAnalysis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prID := 42
	repo := &pullRequestHandlerRepo{
		repo:   &models.Repository{ID: "repo-1", OrganizationID: "org-1", Type: models.RepositoryTypeGitHub, URL: "https://github.com/owner/repo"},
		config: &models.OrganizationConfig{GithubToken: "github-token"},
		latestByPR: map[int]models.CodeAnalysis{
			42: {
				ID:            "analysis-1",
				RepositoryID:  "repo-1",
				PullRequestID: &prID,
				Type:          models.AnalysisTypeCodeReview,
				Status:        models.AnalysisStatusCompleted,
				Issues:        datatypes.NewJSONType([]models.CodeIssue{{File: "main.go", Line: 7, Title: "bug"}}),
				IssueCount:    1,
			},
		},
	}
	gh := &pullRequestMockGitHub{prs: []github.PullRequest{{Number: 42, Title: "fix", User: github.User{Login: "ana"}}}}
	handler := NewAnalysisHandler(repo, &pullRequestHandlerEnqueuer{}, nil, nil)
	handler.githubFactory = func(string) github.ClientInterface { return gh }

	c, w := newPullRequestHandlerTestContext(http.MethodGet, "/repositories/repo-1/pull-requests", "")
	handler.ListPullRequests(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp models.PullRequestListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Total != 1 || resp.Items[0].PullRequest.Number != 42 {
		t.Fatalf("response = %+v, want PR 42", resp)
	}
	if resp.Items[0].LatestAnalysis == nil || resp.Items[0].LatestAnalysis.ID != "analysis-1" {
		t.Fatalf("latest analysis = %+v, want analysis-1", resp.Items[0].LatestAnalysis)
	}
	if len(repo.lastAnalysisBatch) != 1 || repo.lastAnalysisBatch[0] != 42 {
		t.Fatalf("analysis batch = %+v, want [42]", repo.lastAnalysisBatch)
	}
}

func TestAnalysisHandler_GetPullRequest_ReturnsDetailFilesAndAnalysis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prID := 42
	repo := &pullRequestHandlerRepo{
		repo:   &models.Repository{ID: "repo-1", OrganizationID: "org-1", Type: models.RepositoryTypeGitHub, URL: "https://github.com/owner/repo"},
		config: &models.OrganizationConfig{GithubToken: "github-token"},
		latest: &models.CodeAnalysis{
			ID:            "analysis-1",
			RepositoryID:  "repo-1",
			PullRequestID: &prID,
			Type:          models.AnalysisTypeCodeReview,
			Status:        models.AnalysisStatusCompleted,
			Issues:        datatypes.NewJSONType([]models.CodeIssue{}),
		},
	}
	gh := &pullRequestMockGitHub{
		pr:    &github.PullRequest{Number: 42, Title: "fix", Head: github.Branch{Ref: "feature", SHA: "abc"}, Base: github.Branch{Ref: "main"}},
		files: []github.PRFile{{Filename: "main.go", Status: "modified", Patch: "@@ -1 +1 @@"}},
	}
	handler := NewAnalysisHandler(repo, &pullRequestHandlerEnqueuer{}, nil, nil)
	handler.githubFactory = func(string) github.ClientInterface { return gh }

	c, w := newPullRequestHandlerTestContext(http.MethodGet, "/repositories/repo-1/pull-requests/42", "")
	handler.GetPullRequest(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp models.PullRequestDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.PullRequest.HeadBranch != "feature" || resp.PullRequest.HeadSHA != "abc" {
		t.Fatalf("pull request = %+v, want feature abc", resp.PullRequest)
	}
	if len(resp.Files) != 1 || resp.Files[0].Filename != "main.go" {
		t.Fatalf("files = %+v, want main.go", resp.Files)
	}
	if resp.LatestAnalysis == nil || resp.LatestAnalysis.ID != "analysis-1" {
		t.Fatalf("latest analysis = %+v, want analysis-1", resp.LatestAnalysis)
	}
}

func TestAnalysisHandler_AnalyzePullRequest_EnqueuesPRPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &pullRequestHandlerRepo{
		repo:   &models.Repository{ID: "repo-1", OrganizationID: "org-1", Type: models.RepositoryTypeGitHub, URL: "https://github.com/owner/repo"},
		config: &models.OrganizationConfig{GithubToken: "github-token", AnthropicAPIKey: "anthropic", AnthropicTokensPerHour: 1000},
	}
	gh := &pullRequestMockGitHub{pr: &github.PullRequest{Number: 42, Head: github.Branch{Ref: "feature", SHA: "abc"}}}
	enqueuer := &pullRequestHandlerEnqueuer{}
	handler := NewAnalysisHandler(repo, enqueuer, nil, nil)
	handler.githubFactory = func(string) github.ClientInterface { return gh }

	c, w := newPullRequestHandlerTestContext(http.MethodPost, "/repositories/repo-1/pull-requests/42/analyze", "")
	handler.AnalyzePullRequest(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", w.Code, w.Body.String())
	}
	payload, ok := enqueuer.payload.(tasks.AnalyzeRepoPayload)
	if !ok {
		t.Fatalf("payload type = %T, want AnalyzeRepoPayload", enqueuer.payload)
	}
	if payload.PullRequestID != 42 || payload.Branch != "feature" || payload.CommitSHA != "abc" || payload.Type != "code_review" {
		t.Fatalf("payload = %+v, want PR 42 feature abc code_review", payload)
	}
}

func TestAnalysisHandler_CreatePullRequestReview_ApproveAndRequestChangesValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &pullRequestHandlerRepo{
		repo:   &models.Repository{ID: "repo-1", OrganizationID: "org-1", Type: models.RepositoryTypeGitHub, URL: "https://github.com/owner/repo"},
		config: &models.OrganizationConfig{GithubToken: "github-token"},
	}
	gh := &pullRequestMockGitHub{reviewID: 99}
	handler := NewAnalysisHandler(repo, &pullRequestHandlerEnqueuer{}, nil, nil)
	handler.githubFactory = func(string) github.ClientInterface { return gh }

	c, w := newPullRequestHandlerTestContext(http.MethodPost, "/repositories/repo-1/pull-requests/42/reviews", `{"event":"APPROVE"}`)
	handler.CreatePullRequestReview(c)
	if w.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gh.reviewEvent != "APPROVE" {
		t.Fatalf("event = %q, want APPROVE", gh.reviewEvent)
	}

	c, w = newPullRequestHandlerTestContext(http.MethodPost, "/repositories/repo-1/pull-requests/42/reviews", `{"event":"REQUEST_CHANGES"}`)
	handler.CreatePullRequestReview(c)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("request changes status = %d, want 422; body=%s", w.Code, w.Body.String())
	}
}

func TestAnalysisHandler_CreatePullRequestReview_CommentWithInlineComments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &pullRequestHandlerRepo{
		repo:   &models.Repository{ID: "repo-1", OrganizationID: "org-1", Type: models.RepositoryTypeGitHub, URL: "https://github.com/owner/repo"},
		config: &models.OrganizationConfig{GithubToken: "github-token"},
	}
	gh := &pullRequestMockGitHub{reviewID: 100}
	handler := NewAnalysisHandler(repo, &pullRequestHandlerEnqueuer{}, nil, nil)
	handler.githubFactory = func(string) github.ClientInterface { return gh }

	body := `{"event":"COMMENT","body":"review body","comments":[{"path":"main.go","position":3,"body":"fix this"}]}`
	c, w := newPullRequestHandlerTestContext(http.MethodPost, "/repositories/repo-1/pull-requests/42/reviews", body)
	handler.CreatePullRequestReview(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gh.reviewEvent != "COMMENT" || gh.reviewBody != "review body" {
		t.Fatalf("review = %q %q, want COMMENT review body", gh.reviewEvent, gh.reviewBody)
	}
	if len(gh.comments) != 1 || gh.comments[0].Path != "main.go" || gh.comments[0].Position != 3 {
		t.Fatalf("comments = %+v, want main.go position 3", gh.comments)
	}
}
