package workers

import (
	"context"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
)

// recordingEnqueuer captures the task types that were enqueued so tests can
// assert on what a webhook event triggers.
type recordingEnqueuer struct {
	enqueued []string
}

func (e *recordingEnqueuer) Enqueue(ctx context.Context, taskType string, payload any, opts ...asynq.Option) error {
	e.enqueued = append(e.enqueued, taskType)
	return nil
}

func (e *recordingEnqueuer) EnqueueIn(ctx context.Context, taskType string, payload any, delay time.Duration, opts ...asynq.Option) error {
	e.enqueued = append(e.enqueued, taskType)
	return nil
}

func (e *recordingEnqueuer) has(taskType string) bool {
	for _, t := range e.enqueued {
		if t == taskType {
			return true
		}
	}
	return false
}

// A push event must only trigger a repository sync. Automatic AI code analysis
// and dependency scanning on push were removed for the MVP; this test locks
// that behavior in so they cannot silently come back.
func TestProcessEvent_PushEnqueuesOnlySync(t *testing.T) {
	enq := &recordingEnqueuer{}
	repo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			// Zero-value EmbeddingsStatus (not "indexed") keeps the stale-index
			// branch dormant, isolating the enqueue behavior under test.
			return &models.Repository{}, nil
		},
	}
	wp := NewWebhookProcessor(repo, nil, enq)

	webhook := &models.Webhook{
		RepositoryID: "repo-1",
		EventType:    models.WebhookEventPush,
		EventPayload: models.WebhookEventPayload{Branch: "main", CommitSHA: "abc123"},
	}

	if err := wp.processEvent(context.Background(), webhook); err != nil {
		t.Fatalf("processEvent returned error: %v", err)
	}

	if !enq.has(tasks.TypeSyncRepo) {
		t.Errorf("expected a sync job to be enqueued on push, got %v", enq.enqueued)
	}
	if enq.has(tasks.TypeAnalyzeRepo) {
		t.Errorf("push must NOT enqueue code analysis anymore, got %v", enq.enqueued)
	}
}

// PR events used to auto-trigger analysis. That auto-trigger was disabled for
// the MVP (the PR-review pipeline is kept dormant behind manual endpoints), so
// a pull_request webhook must not enqueue any job.
func TestProcessEvent_PullRequestEnqueuesNothing(t *testing.T) {
	enq := &recordingEnqueuer{}
	repo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
			return &models.Repository{}, nil
		},
	}
	wp := NewWebhookProcessor(repo, nil, enq)

	prID := 42
	webhook := &models.Webhook{
		RepositoryID: "repo-1",
		EventType:    models.WebhookEventPullRequest,
		EventPayload: models.WebhookEventPayload{Branch: "feature", CommitSHA: "def456", PullRequestID: &prID},
	}

	if err := wp.processEvent(context.Background(), webhook); err != nil {
		t.Fatalf("processEvent returned error: %v", err)
	}

	if len(enq.enqueued) != 0 {
		t.Errorf("pull_request events must not enqueue any job now, got %v", enq.enqueued)
	}
}
