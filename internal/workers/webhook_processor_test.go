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

// A fake stands in for a live queue, so Available is true — the false case is
// what the no-op implementation exists to model, and a test that wants it says so
// explicitly.
func (e *recordingEnqueuer) Available() bool { return true }

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

// A push event must only trigger a repository sync. Every AI-driven trigger
// (code analysis, dependency scanning, embedding indexing) has been removed;
// this test locks that behavior in so none of them can silently come back.
func TestProcessEvent_PushEnqueuesOnlySync(t *testing.T) {
	enq := &recordingEnqueuer{}
	repo := &mockRepository{
		getRepoFunc: func(ctx context.Context, id string) (*models.Repository, error) {
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
	if len(enq.enqueued) != 1 {
		t.Errorf("push must enqueue the sync job and nothing else, got %v", enq.enqueued)
	}
}

// PR events used to auto-trigger AI analysis. The PR-review pipeline is gone
// entirely, so a pull_request webhook must not enqueue any job.
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
