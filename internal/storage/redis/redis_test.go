package redis_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/paulozy/idp-with-ai-backend/internal/config"
	redisstore "github.com/paulozy/idp-with-ai-backend/internal/storage/redis"
)

func newTestConfig(t *testing.T) (*miniredis.Miniredis, *config.RedisConfig) {
	t.Helper()
	mr := miniredis.RunT(t)
	_, portStr, _ := net.SplitHostPort(mr.Addr())
	port, _ := strconv.Atoi(portStr)
	cfg := &config.RedisConfig{
		Host:     "127.0.0.1",
		Port:     port,
		DB:       0,
		Password: "",
	}
	return mr, cfg
}

// ── Client tests ──────────────────────────────────────────────────────────────

func TestNewRedisClient_Ping_Success(t *testing.T) {
	_, cfg := newTestConfig(t)
	client, err := redisstore.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer client.Close()

	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

func TestNewRedisClient_UnreachableHost_ReturnsError(t *testing.T) {
	cfg := &config.RedisConfig{Host: "127.0.0.1", Port: 19999, DB: 0}
	_, err := redisstore.New(cfg)
	if err == nil {
		t.Error("expected error for unreachable host, got nil")
	}
}

func TestNoopClient_PingAndClose_NeverError(t *testing.T) {
	client := redisstore.NewNoop()
	if err := client.Ping(context.Background()); err != nil {
		t.Errorf("Noop Ping: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Errorf("Noop Close: %v", err)
	}
	if client.Client() != nil {
		t.Error("Noop Client() should return nil")
	}
}

// ── Cache tests ───────────────────────────────────────────────────────────────

func TestCache_SetGet_HappyPath(t *testing.T) {
	_, cfg := newTestConfig(t)
	client, _ := redisstore.New(cfg)
	defer client.Close()
	cache := redisstore.NewRedisCache(client)

	if err := cache.Set(context.Background(), "k", "v", time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := cache.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "v" {
		t.Errorf("expected 'v', got %q", got)
	}
}

func TestCache_Get_Miss_ReturnsErrCacheMiss(t *testing.T) {
	_, cfg := newTestConfig(t)
	client, _ := redisstore.New(cfg)
	defer client.Close()
	cache := redisstore.NewRedisCache(client)

	_, err := cache.Get(context.Background(), "nonexistent")
	if !errors.Is(err, redisstore.ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}

func TestCache_Del_RemovesKey(t *testing.T) {
	_, cfg := newTestConfig(t)
	client, _ := redisstore.New(cfg)
	defer client.Close()
	cache := redisstore.NewRedisCache(client)

	_ = cache.Set(context.Background(), "k", "v", time.Minute)
	if err := cache.Del(context.Background(), "k"); err != nil {
		t.Fatalf("Del: %v", err)
	}
	_, err := cache.Get(context.Background(), "k")
	if !errors.Is(err, redisstore.ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss after Del, got %v", err)
	}
}

func TestCache_Exists_TrueAndFalse(t *testing.T) {
	_, cfg := newTestConfig(t)
	client, _ := redisstore.New(cfg)
	defer client.Close()
	cache := redisstore.NewRedisCache(client)

	ok, err := cache.Exists(context.Background(), "missing")
	if err != nil || ok {
		t.Errorf("expected false for missing key, got ok=%v err=%v", ok, err)
	}

	_ = cache.Set(context.Background(), "present", "1", time.Minute)
	ok, err = cache.Exists(context.Background(), "present")
	if err != nil || !ok {
		t.Errorf("expected true for present key, got ok=%v err=%v", ok, err)
	}
}

func TestNoopCache_NeverErrors(t *testing.T) {
	cache := redisstore.NewNoopCache()
	ctx := context.Background()

	_, err := cache.Get(ctx, "k")
	if !errors.Is(err, redisstore.ErrCacheMiss) {
		t.Errorf("noop Get should return ErrCacheMiss, got %v", err)
	}
	if err := cache.Set(ctx, "k", "v", time.Minute); err != nil {
		t.Errorf("noop Set: %v", err)
	}
	if err := cache.Del(ctx, "k"); err != nil {
		t.Errorf("noop Del: %v", err)
	}
	ok, err := cache.Exists(ctx, "k")
	if err != nil || ok {
		t.Errorf("noop Exists: ok=%v err=%v", ok, err)
	}
}
