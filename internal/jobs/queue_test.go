package jobs_test

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
)

func newTestConfig(t *testing.T) *config.RedisConfig {
	t.Helper()
	mr := miniredis.RunT(t)
	_, portStr, _ := net.SplitHostPort(mr.Addr())
	port, _ := strconv.Atoi(portStr)
	return &config.RedisConfig{
		Host:     "127.0.0.1",
		Port:     port,
		DB:       0,
		Password: "",
	}
}

type samplePayload struct {
	RepoID string `json:"repo_id"`
}

func TestAsynqEnqueuer_Enqueue_Success(t *testing.T) {
	cfg := newTestConfig(t)
	enqueuer := jobs.NewAsynqEnqueuer(cfg)

	err := enqueuer.Enqueue(context.Background(), "repo:analyze", samplePayload{RepoID: "abc123"})
	if err != nil {
		t.Errorf("Enqueue: %v", err)
	}
}

func TestAsynqEnqueuer_EnqueueIn_Success(t *testing.T) {
	cfg := newTestConfig(t)
	enqueuer := jobs.NewAsynqEnqueuer(cfg)

	err := enqueuer.EnqueueIn(context.Background(), "repo:analyze", samplePayload{RepoID: "delayed"}, 0)
	if err != nil {
		t.Errorf("EnqueueIn: %v", err)
	}
}

func TestNoopEnqueuer_NeverErrors(t *testing.T) {
	enqueuer := jobs.NewNoopEnqueuer()
	ctx := context.Background()

	if err := enqueuer.Enqueue(ctx, "repo:analyze", samplePayload{}); err != nil {
		t.Errorf("noop Enqueue: %v", err)
	}
	if err := enqueuer.EnqueueIn(ctx, "repo:analyze", samplePayload{}, 0); err != nil {
		t.Errorf("noop EnqueueIn: %v", err)
	}
}
