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
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type webhookHandlerRepo struct {
	storage.Repository
	config    *models.WebhookConfig
	byDeliver map[string]*models.Webhook
	created   []*models.Webhook
}

func (r *webhookHandlerRepo) GetWebhookConfigByRepoID(context.Context, string) (*models.WebhookConfig, error) {
	return r.config, nil
}

func (r *webhookHandlerRepo) GetWebhookByDeliveryID(_ context.Context, id string) (*models.Webhook, error) {
	return r.byDeliver[id], nil
}

func (r *webhookHandlerRepo) CreateWebhook(_ context.Context, webhook *models.Webhook) error {
	webhook.ID = "wh-" + webhook.DeliveryID
	r.created = append(r.created, webhook)
	if r.byDeliver == nil {
		r.byDeliver = map[string]*models.Webhook{}
	}
	r.byDeliver[webhook.DeliveryID] = webhook
	return nil
}

type recordingEnqueuer struct {
	tasks []string
}

func (e *recordingEnqueuer) Enqueue(_ context.Context, taskType string, _ any, _ ...asynq.Option) error {
	e.tasks = append(e.tasks, taskType)
	return nil
}

func (e *recordingEnqueuer) EnqueueIn(_ context.Context, taskType string, _ any, _ time.Duration, _ ...asynq.Option) error {
	e.tasks = append(e.tasks, taskType)
	return nil
}

// postGitLabWebhook drives the receiver through a real router, so the status
// gin defers until the response is flushed is the one the test observes.
func postGitLabWebhook(t *testing.T, h *WebhookHandler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/webhooks/gitlab/:repoID", h.HandleGitLabWebhook)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gitlab/repo-1", strings.NewReader(body))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

const gitlabPushBody = `{
	"object_kind": "push",
	"ref": "refs/heads/main",
	"checkout_sha": "9a1b2c3",
	"user_name": "Dev",
	"project": {"id": 250833, "path_with_namespace": "group/subgroup/project"}
}`

func newGitLabWebhookHandler(secret string) (*WebhookHandler, *webhookHandlerRepo, *recordingEnqueuer) {
	repo := &webhookHandlerRepo{
		config:    &models.WebhookConfig{RepositoryID: "repo-1", Secret: secret, IsActive: true},
		byDeliver: map[string]*models.Webhook{},
	}
	enqueuer := &recordingEnqueuer{}
	return NewWebhookHandler(repo, enqueuer), repo, enqueuer
}

func TestHandleGitLabWebhook_AcceptsValidToken(t *testing.T) {
	h, repo, enqueuer := newGitLabWebhookHandler("s3cret")

	w := postGitLabWebhook(t, h, gitlabPushBody, map[string]string{
		"X-Gitlab-Token":      "s3cret",
		"X-Gitlab-Event":      "Push Hook",
		"X-Gitlab-Event-UUID": "delivery-1",
	})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", w.Code, w.Body.String())
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d webhooks, want 1", len(repo.created))
	}
	got := repo.created[0]
	if got.EventType != models.WebhookEventPush {
		t.Errorf("EventType = %q, want push", got.EventType)
	}
	if got.EventPayload.Provider != "gitlab" {
		t.Errorf("Provider = %q, want gitlab", got.EventPayload.Provider)
	}
	if got.EventPayload.Branch != "main" {
		t.Errorf("Branch = %q, want main (refs/heads/ stripped)", got.EventPayload.Branch)
	}
	if got.EventPayload.CommitSHA != "9a1b2c3" {
		t.Errorf("CommitSHA = %q, want the checkout_sha", got.EventPayload.CommitSHA)
	}
	if got.EventPayload.RepositoryName != "group/subgroup/project" {
		t.Errorf("RepositoryName = %q, want the full nested path", got.EventPayload.RepositoryName)
	}
	// A push must reach the same processor the GitHub receiver feeds, which is
	// what makes a GitLab push refresh the catalog.
	if len(enqueuer.tasks) != 1 || enqueuer.tasks[0] != tasks.TypeProcessWebhook {
		t.Errorf("enqueued %v, want one %s", enqueuer.tasks, tasks.TypeProcessWebhook)
	}
}

func TestHandleGitLabWebhook_RejectsBadToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "wrong token", token: "wrong"},
		// A missing header must never be treated as "no token required".
		{name: "missing token", token: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo, enqueuer := newGitLabWebhookHandler("s3cret")

			headers := map[string]string{"X-Gitlab-Event": "Push Hook"}
			if tt.token != "" {
				headers["X-Gitlab-Token"] = tt.token
			}
			w := postGitLabWebhook(t, h, gitlabPushBody, headers)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if len(repo.created) != 0 || len(enqueuer.tasks) != 0 {
				t.Error("an unauthenticated delivery was recorded or enqueued")
			}
		})
	}
}

func TestHandleGitLabWebhook_UnconfiguredRepositoryIsRejected(t *testing.T) {
	repo := &webhookHandlerRepo{}
	h := NewWebhookHandler(repo, &recordingEnqueuer{})

	w := postGitLabWebhook(t, h, gitlabPushBody, map[string]string{"X-Gitlab-Token": "anything"})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when no webhook is configured", w.Code)
	}
}

func TestHandleGitLabWebhook_DropsDuplicateDeliveries(t *testing.T) {
	h, repo, enqueuer := newGitLabWebhookHandler("s3cret")
	headers := map[string]string{
		"X-Gitlab-Token":      "s3cret",
		"X-Gitlab-Event":      "Push Hook",
		"X-Gitlab-Event-UUID": "delivery-1",
	}

	if w := postGitLabWebhook(t, h, gitlabPushBody, headers); w.Code != http.StatusAccepted {
		t.Fatalf("first delivery status = %d, want 202", w.Code)
	}
	w := postGitLabWebhook(t, h, gitlabPushBody, headers)

	if w.Code != http.StatusOK {
		t.Fatalf("replayed delivery status = %d, want 200", w.Code)
	}
	if len(repo.created) != 1 || len(enqueuer.tasks) != 1 {
		t.Errorf("created %d / enqueued %d, want the replay ignored", len(repo.created), len(enqueuer.tasks))
	}
}

func TestHandleGitLabWebhook_DerivesDeliveryIDWithoutTheUUIDHeader(t *testing.T) {
	h, repo, enqueuer := newGitLabWebhookHandler("s3cret")
	// Older GitLab instances send no X-Gitlab-Event-UUID. A retry of the same
	// event must still be recognized as the same event.
	headers := map[string]string{"X-Gitlab-Token": "s3cret", "X-Gitlab-Event": "Push Hook"}

	first := postGitLabWebhook(t, h, gitlabPushBody, headers)
	second := postGitLabWebhook(t, h, gitlabPushBody, headers)

	if first.Code != http.StatusAccepted || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d/%d, want 202 then 200", first.Code, second.Code)
	}
	if len(repo.created) != 1 || len(enqueuer.tasks) != 1 {
		t.Fatalf("created %d / enqueued %d, want the retry deduplicated", len(repo.created), len(enqueuer.tasks))
	}
	// The key must identify this repository's event, not just the payload:
	// delivery IDs are globally unique.
	if id := repo.created[0].DeliveryID; !strings.Contains(id, "repo-1") || !strings.Contains(id, "9a1b2c3") {
		t.Errorf("DeliveryID = %q, want it derived from repository and commit", id)
	}
}

func TestHandleGitLabWebhook_NormalizesMergeRequestEvents(t *testing.T) {
	h, repo, _ := newGitLabWebhookHandler("s3cret")
	body := `{
		"object_kind": "merge_request",
		"project": {"id": 250833, "path_with_namespace": "group/project"},
		"user": {"username": "dev"},
		"object_attributes": {
			"iid": 7222,
			"action": "open",
			"source_branch": "fix/typo",
			"target_branch": "main",
			"last_commit": {"id": "d094172f", "message": "fix: typo"}
		}
	}`

	w := postGitLabWebhook(t, h, body, map[string]string{
		"X-Gitlab-Token": "s3cret",
		"X-Gitlab-Event": "Merge Request Hook",
	})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	got := repo.created[0]
	if got.EventType != models.WebhookEventMergeRequest {
		t.Errorf("EventType = %q, want merge_request", got.EventType)
	}
	// The worker reads PullRequestNumber; GitLab's iid is the same concept, so
	// nothing downstream needs a GitLab-specific branch.
	if got.EventPayload.PullRequestNumber == nil || *got.EventPayload.PullRequestNumber != 7222 {
		t.Errorf("PullRequestNumber = %v, want 7222", got.EventPayload.PullRequestNumber)
	}
	if got.EventPayload.Branch != "fix/typo" || got.EventPayload.CommitSHA != "d094172f" {
		t.Errorf("branch/sha = %q/%q, want the source branch and last commit", got.EventPayload.Branch, got.EventPayload.CommitSHA)
	}
	if got.EventPayload.ActorName != "dev" {
		t.Errorf("ActorName = %q, want dev", got.EventPayload.ActorName)
	}
}

func TestResolveGitLabEventType(t *testing.T) {
	tests := []struct {
		name       string
		objectKind string
		header     string
		want       models.WebhookEventType
	}{
		{name: "push", objectKind: "push", header: "Push Hook", want: models.WebhookEventPush},
		{name: "merge request", objectKind: "merge_request", header: "Merge Request Hook", want: models.WebhookEventMergeRequest},
		{name: "tag push", objectKind: "tag_push", header: "Tag Push Hook", want: models.WebhookEventTag},
		{name: "issue", objectKind: "issue", header: "Issue Hook", want: models.WebhookEventIssue},
		{name: "pipeline", objectKind: "pipeline", header: "Pipeline Hook", want: models.WebhookEventPipeline},
		// object_kind wins: the header is a display name whose formatting has
		// changed between GitLab versions.
		{name: "header only", objectKind: "", header: "Merge Request Hook", want: models.WebhookEventMergeRequest},
		{name: "unrecognized", objectKind: "deployment", header: "Deployment Hook", want: models.WebhookEventUnknown},
		{name: "nothing", objectKind: "", header: "", want: models.WebhookEventUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveGitLabEventType(tt.objectKind, tt.header); got != tt.want {
				t.Fatalf("resolveGitLabEventType(%q, %q) = %q, want %q", tt.objectKind, tt.header, got, tt.want)
			}
		})
	}
}

func TestHandleGitLabWebhook_UnparsablePayloadIsStillRecorded(t *testing.T) {
	h, repo, _ := newGitLabWebhookHandler("s3cret")

	// The token proves the sender is GitLab. Dropping an authenticated
	// delivery because the body surprised us would lose the event entirely;
	// recording it as unknown keeps it inspectable.
	w := postGitLabWebhook(t, h, `not json`, map[string]string{
		"X-Gitlab-Token": "s3cret",
		"X-Gitlab-Event": "Push Hook",
	})

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if len(repo.created) != 1 {
		t.Fatalf("created %d webhooks, want the delivery recorded", len(repo.created))
	}
	if repo.created[0].EventType != models.WebhookEventPush {
		t.Errorf("EventType = %q, want the header to still resolve push", repo.created[0].EventType)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(repo.created[0].EventPayload.RawData["body"].(string)), &raw); err == nil {
		t.Error("expected the raw body to be preserved verbatim, including invalid JSON")
	}
}
