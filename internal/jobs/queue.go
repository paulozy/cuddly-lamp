package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// Enqueuer is the interface for dispatching background jobs.
// Callers depend on this interface so they stay decoupled from asynq internals.
type Enqueuer interface {
	Enqueue(ctx context.Context, taskType string, payload any, opts ...asynq.Option) error
	EnqueueIn(ctx context.Context, taskType string, payload any, delay time.Duration, opts ...asynq.Option) error
}

// ── asynq implementation ──────────────────────────────────────────────────────

type asynqEnqueuer struct {
	client *asynq.Client
}

// NewAsynqEnqueuer creates an Enqueuer backed by asynq + Redis.
func NewAsynqEnqueuer(cfg *config.RedisConfig) Enqueuer {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client := asynq.NewClient(asynq.RedisClientOpt{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return &asynqEnqueuer{client: client}
}

func (e *asynqEnqueuer) Enqueue(ctx context.Context, taskType string, payload any, opts ...asynq.Option) error {
	task, err := newTask(taskType, payload)
	if err != nil {
		return err
	}
	_, err = e.client.EnqueueContext(ctx, task, opts...)
	return err
}

func (e *asynqEnqueuer) EnqueueIn(ctx context.Context, taskType string, payload any, delay time.Duration, opts ...asynq.Option) error {
	task, err := newTask(taskType, payload)
	if err != nil {
		return err
	}
	opts = append(opts, asynq.ProcessIn(delay))
	_, err = e.client.EnqueueContext(ctx, task, opts...)
	return err
}

func newTask(taskType string, payload any) (*asynq.Task, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal task payload: %w", err)
	}
	return asynq.NewTask(taskType, data), nil
}

// ── No-op implementation ──────────────────────────────────────────────────────

type noopEnqueuer struct{}

// NewNoopEnqueuer returns an Enqueuer that logs the job but does not execute it.
// Used as a fallback when Redis is unavailable.
func NewNoopEnqueuer() Enqueuer {
	return &noopEnqueuer{}
}

func (n *noopEnqueuer) Enqueue(_ context.Context, taskType string, _ any, _ ...asynq.Option) error {
	utils.Warn("Noop enqueuer: job dropped", "type", taskType)
	return nil
}

func (n *noopEnqueuer) EnqueueIn(_ context.Context, taskType string, _ any, delay time.Duration, _ ...asynq.Option) error {
	utils.Warn("Noop enqueuer: delayed job dropped", "type", taskType, "delay", delay)
	return nil
}
