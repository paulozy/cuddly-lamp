package redis

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrCacheMiss is returned by Cache.Get when the key does not exist.
var ErrCacheMiss = errors.New("cache miss")

// Cache is the interface for key/value caching operations.
// Callers must check errors.Is(err, ErrCacheMiss) to distinguish a missing key
// from a Redis failure.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Exists(ctx context.Context, key string) (bool, error)
}

// ── Redis implementation ──────────────────────────────────────────────────────

type redisCache struct {
	client RedisClient
}

// NewRedisCache returns a Cache backed by a live Redis connection.
func NewRedisCache(client RedisClient) Cache {
	return &redisCache{client: client}
}

func (c *redisCache) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Client().Get(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (c *redisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return c.client.Client().Set(ctx, key, value, ttl).Err()
}

func (c *redisCache) Del(ctx context.Context, keys ...string) error {
	return c.client.Client().Del(ctx, keys...).Err()
}

func (c *redisCache) Exists(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Client().Exists(ctx, key).Result()
	return n > 0, err
}

// ── No-op implementation ──────────────────────────────────────────────────────

type noopCache struct{}

// NewNoopCache returns a Cache that always reports a miss and silently ignores writes.
// Used when Redis is unavailable.
func NewNoopCache() Cache {
	return &noopCache{}
}

func (n *noopCache) Get(_ context.Context, _ string) (string, error) {
	return "", ErrCacheMiss
}

func (n *noopCache) Set(_ context.Context, _, _ string, _ time.Duration) error { return nil }
func (n *noopCache) Del(_ context.Context, _ ...string) error                   { return nil }
func (n *noopCache) Exists(_ context.Context, _ string) (bool, error)           { return false, nil }
