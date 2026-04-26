package jobs

import (
	"context"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// Worker wraps an asynq.Server and its handler mux.
// Register task handlers before calling Run.
type Worker struct {
	server *asynq.Server
	mux    *asynq.ServeMux
}

// NewWorker creates a Worker connected to Redis.
// It does not start processing until Run is called.
func NewWorker(cfg *config.RedisConfig) *Worker {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	server := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		},
		asynq.Config{
			// Weighted priority queues: critical > default > low
			Queues: map[string]int{
				"critical": 6,
				"default":  3,
				"low":      1,
			},
			Concurrency:    10,
			RetryDelayFunc: asynq.DefaultRetryDelayFunc,
			ErrorHandler: asynq.ErrorHandlerFunc(func(_ context.Context, task *asynq.Task, err error) {
				utils.Error("Job failed", "type", task.Type(), "error", err)
			}),
		},
	)

	return &Worker{
		server: server,
		mux:    asynq.NewServeMux(),
	}
}

// Register wires a handler function for the given task type.
// Must be called before Run.
func (w *Worker) Register(taskType string, handler asynq.HandlerFunc) {
	w.mux.HandleFunc(taskType, handler)
}

// Run starts the worker loop. This call blocks until Shutdown is called.
// Intended to run in a goroutine: go worker.Run()
func (w *Worker) Run() error {
	utils.Info("Starting job worker")
	return w.server.Run(w.mux)
}

// Shutdown gracefully stops the worker, waiting for in-flight jobs to finish.
func (w *Worker) Shutdown() {
	utils.Info("Shutting down job worker")
	w.server.Shutdown()
}
