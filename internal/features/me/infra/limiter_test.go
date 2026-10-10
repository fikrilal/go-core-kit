package infra_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/fikrilal/go-core-kit/internal/features/me/infra"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func TestUploadLimiterBlocksAtTheThreshold(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.MustOpen("redis://" + mini.Addr() + "/0")
	t.Cleanup(func() { _ = client.Close() })

	limiter := infra.NewUploadLimiter(client,
		infra.UploadLimitConfig{MaxAttempts: 2, Window: time.Minute, Block: time.Minute},
		infra.UploadLimitConfig{MaxAttempts: 10, Window: time.Minute, Block: time.Minute},
	)
	ctx := t.Context()

	if err := limiter.AssertAllowed(ctx, "user-1", "203.0.113.10"); err != nil {
		t.Fatalf("first attempt error = %v", err)
	}
	if err := limiter.AssertAllowed(ctx, "user-1", "203.0.113.10"); err != nil {
		t.Fatalf("second attempt error = %v", err)
	}
	err := limiter.AssertAllowed(ctx, "user-1", "203.0.113.10")
	if err == nil {
		t.Fatal("third attempt = nil, want the user block to apply")
	}
	var problem *httpx.ProblemError
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	if problem.Status != http.StatusTooManyRequests || problem.Code != httpx.CodeRateLimited {
		t.Fatalf("problem = %+v, want 429 RATE_LIMITED", problem)
	}
	if problem.RetryAfterSeconds < 1 {
		t.Errorf("retryAfter = %d, want the block TTL", problem.RetryAfterSeconds)
	}
}

func TestUploadLimiterCountsIPWindow(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.MustOpen("redis://" + mini.Addr() + "/0")
	t.Cleanup(func() { _ = client.Close() })

	limiter := infra.NewUploadLimiter(client,
		infra.UploadLimitConfig{MaxAttempts: 100, Window: time.Minute, Block: time.Minute},
		infra.UploadLimitConfig{MaxAttempts: 1, Window: time.Minute, Block: time.Minute},
	)
	ctx := t.Context()
	if err := limiter.AssertAllowed(ctx, "user-1", "203.0.113.10"); err != nil {
		t.Fatalf("first attempt error = %v", err)
	}
	if err := limiter.AssertAllowed(ctx, "user-2", "203.0.113.10"); err == nil {
		t.Fatal("second IP attempt = nil, want the IP block to apply")
	}
}
