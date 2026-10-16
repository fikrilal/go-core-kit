package infra

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

// LimiterConfig parameterizes a limiter: the Redis key scope and what the 429
// says. Two routes with different scopes must not share either.
type LimiterConfig struct {
	Scope       string
	Detail      string
	MaxAttempts int
	Window      time.Duration
	Block       time.Duration
}

// LoginLimiter enforces one attempt policy in Redis.
type LoginLimiter struct {
	client      *redis.Client
	scope       string
	detail      string
	maxAttempts int64
	window      time.Duration
	block       time.Duration
}

// NewLoginLimiter builds the login limiter.
func NewLoginLimiter(client *redis.Client, maxAttempts int, window, block time.Duration) *LoginLimiter {
	return NewLimiter(client, LimiterConfig{
		Scope:       "auth:login",
		Detail:      "Too many login attempts. Try again later.",
		MaxAttempts: maxAttempts,
		Window:      window,
		Block:       block,
	})
}

// NewLimiter builds a limiter under its own scope and message.
func NewLimiter(client *redis.Client, config LimiterConfig) *LoginLimiter {
	return &LoginLimiter{
		client:      client,
		scope:       config.Scope,
		detail:      config.Detail,
		maxAttempts: int64(config.MaxAttempts),
		window:      config.Window,
		block:       config.Block,
	}
}

type attemptKeys struct {
	count string
	block string
}

// AssertAllowed rejects logins while an email or IP block is active.
func (l *LoginLimiter) AssertAllowed(ctx context.Context, email, ip string) error {
	retryAfter := time.Duration(0)
	for _, pair := range l.keys(email, ip) {
		_, found, err := l.client.Get(ctx, pair.block)
		if err != nil {
			return fmt.Errorf("auth rate limit: %w", err)
		}
		if !found {
			continue
		}
		ttl, err := l.client.TTL(ctx, pair.block)
		if err != nil {
			return fmt.Errorf("auth rate limit: %w", err)
		}
		if ttl <= 0 {
			ttl = l.block
		}
		if ttl > retryAfter {
			retryAfter = ttl
		}
	}
	if retryAfter <= 0 {
		return nil
	}
	return &httpx.ProblemError{
		Status:            http.StatusTooManyRequests,
		Code:              httpx.CodeRateLimited,
		Detail:            l.detail,
		RetryAfterSeconds: int(retryAfter.Round(time.Second).Seconds()),
	}
}

// RecordFailure counts a failed attempt and blocks the subject at the threshold.
func (l *LoginLimiter) RecordFailure(ctx context.Context, email, ip string) error {
	for _, pair := range l.keys(email, ip) {
		count, err := l.client.Incr(ctx, pair.count)
		if err != nil {
			return fmt.Errorf("auth rate limit: %w", err)
		}
		if count == 1 {
			if err := l.client.Expire(ctx, pair.count, l.window); err != nil {
				return fmt.Errorf("auth rate limit: %w", err)
			}
		}
		if count >= l.maxAttempts {
			if err := l.client.Set(ctx, pair.block, "1", l.block); err != nil {
				return fmt.Errorf("auth rate limit: %w", err)
			}
		}
	}
	return nil
}

// RecordSuccess clears the failure counters for the subject.
func (l *LoginLimiter) RecordSuccess(ctx context.Context, email, ip string) error {
	counts := make([]string, 0, 2)
	for _, pair := range l.keys(email, ip) {
		counts = append(counts, pair.count)
	}
	if err := l.client.Del(ctx, counts...); err != nil {
		return fmt.Errorf("auth rate limit: %w", err)
	}
	return nil
}

func (l *LoginLimiter) keys(email, ip string) []attemptKeys {
	// The scope decides the namespace: two routes sharing one would let a
	// resend attempt block a login.
	scope := strings.Split(l.scope, ":")
	emailHash := hashKey(email)
	keys := []attemptKeys{{
		count: redis.Key(append(append([]string{}, scope...), "email", emailHash, "failures")...),
		block: redis.Key(append(append([]string{}, scope...), "email", emailHash, "blocked")...),
	}}
	if ip != "" {
		ipHash := hashKey(ip)
		keys = append(keys, attemptKeys{
			count: redis.Key(append(append([]string{}, scope...), "ip", ipHash, "failures")...),
			block: redis.Key(append(append([]string{}, scope...), "ip", ipHash, "blocked")...),
		})
	}
	return keys
}

func hashKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
