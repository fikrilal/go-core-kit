package infra_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/fikrilal/go-core-kit/internal/features/auth/infra"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

const (
	maxAttempts = 10
	window      = time.Minute
	block       = 15 * time.Minute
)

func testLimiter(t *testing.T) *infra.LoginLimiter {
	t.Helper()
	client := redis.MustOpen("redis://" + miniredis.RunT(t).Addr() + "/0")
	t.Cleanup(func() { _ = client.Close() })
	return infra.NewLoginLimiter(client, maxAttempts, window, block)
}

func rateLimited(t *testing.T, err error) *httpx.ProblemError {
	t.Helper()
	var problem *httpx.ProblemError
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want a problem error", err)
	}
	return problem
}

func TestLoginLimiterBlocksAtThreshold(t *testing.T) {
	limiter := testLimiter(t)
	ctx := context.Background()

	for i := 0; i < maxAttempts-1; i++ {
		if err := limiter.RecordFailure(ctx, "user@example.com", "203.0.113.9"); err != nil {
			t.Fatalf("RecordFailure() error = %v", err)
		}
		if err := limiter.AssertAllowed(ctx, "user@example.com", "203.0.113.9"); err != nil {
			t.Fatalf("AssertAllowed() after %d failures error = %v, want nil", i+1, err)
		}
	}
	if err := limiter.RecordFailure(ctx, "user@example.com", "203.0.113.9"); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}
	problem := rateLimited(t, limiter.AssertAllowed(ctx, "user@example.com", "203.0.113.9"))
	if problem.Status != 429 || problem.Code != httpx.CodeRateLimited {
		t.Fatalf("problem = %+v, want 429 RATE_LIMITED", problem)
	}
	if problem.RetryAfterSeconds <= 0 || problem.RetryAfterSeconds > int(block.Seconds()) {
		t.Errorf("retry after = %d, want within (0, %d]", problem.RetryAfterSeconds, int(block.Seconds()))
	}
	if err := limiter.AssertAllowed(ctx, "other@example.com", "203.0.113.9"); err == nil {
		t.Error("a different email from the same blocked IP was allowed")
	}
}

func TestLoginLimiterSuccessClearsCounters(t *testing.T) {
	limiter := testLimiter(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := limiter.RecordFailure(ctx, "user@example.com", "203.0.113.9"); err != nil {
			t.Fatalf("RecordFailure() error = %v", err)
		}
	}
	if err := limiter.RecordSuccess(ctx, "user@example.com", "203.0.113.9"); err != nil {
		t.Fatalf("RecordSuccess() error = %v", err)
	}
	for i := 0; i < maxAttempts-1; i++ {
		if err := limiter.RecordFailure(ctx, "user@example.com", "203.0.113.9"); err != nil {
			t.Fatalf("RecordFailure() error = %v", err)
		}
	}
	if err := limiter.AssertAllowed(ctx, "user@example.com", "203.0.113.9"); err != nil {
		t.Fatalf("AssertAllowed() after a successful login + %d failures error = %v, want nil (counters must reset)", maxAttempts-1, err)
	}
}

func TestLoginLimiterImmediateBlockAfterThreshold(t *testing.T) {
	limiter := testLimiter(t)
	ctx := context.Background()
	for i := 0; i < maxAttempts; i++ {
		if err := limiter.RecordFailure(ctx, "user@example.com", ""); err != nil {
			t.Fatalf("RecordFailure() error = %v", err)
		}
	}
	if err := limiter.AssertAllowed(ctx, "user@example.com", ""); err == nil {
		t.Fatal("AssertAllowed() = nil after the threshold, want a 429")
	}
}
