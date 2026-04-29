package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	goredis "github.com/redis/go-redis/v9"
)

// RedisClient is the base interface for all Redis operations.
// Consumers should depend on this interface, not on *goredis.Client directly.
type RedisClient interface {
	Ping(ctx context.Context) error
	Close() error
	// Client exposes the underlying *goredis.Client for asynq and advanced use.
	Client() *goredis.Client
}

type redisClient struct {
	rdb *goredis.Client
}

// New creates a Redis client and validates the connection with Ping.
// Returns an error if Redis is unreachable — the caller decides whether to abort
// or fall back to NewNoop().
func New(cfg *config.RedisConfig) (RedisClient, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	utils.Info("Connecting to Redis", "addr", addr, "db", cfg.DB)

	rdb := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	utils.Info("Redis connection established", "addr", addr)
	return &redisClient{rdb: rdb}, nil
}

func (c *redisClient) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *redisClient) Close() error {
	return c.rdb.Close()
}

func (c *redisClient) Client() *goredis.Client {
	return c.rdb
}

// ── No-op implementation ──────────────────────────────────────────────────────

type noopClient struct{}

// NewNoop returns a RedisClient that does nothing.
// Used as a fallback when Redis is unavailable and the app must still start.
func NewNoop() RedisClient {
	utils.Warn("Using no-op Redis client — cache and queue will not function")
	return &noopClient{}
}

func (n *noopClient) Ping(_ context.Context) error { return nil }
func (n *noopClient) Close() error                 { return nil }
func (n *noopClient) Client() *goredis.Client      { return nil }
