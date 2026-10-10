package infra

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

// UploadLimitConfig is one limiter's attempts, window, and block.
type UploadLimitConfig struct {
	MaxAttempts int
	Window      time.Duration
	Block       time.Duration
}

// UploadLimiter guards profile image uploads per user and per IP, following the
// reference's key shapes and block-first ordering.
type UploadLimiter struct {
	client *redis.Client
	user   UploadLimitConfig
	ip     UploadLimitConfig
}

// NewUploadLimiter builds the limiter.
func NewUploadLimiter(client *redis.Client, user, ip UploadLimitConfig) *UploadLimiter {
	return &UploadLimiter{client: client, user: user, ip: ip}
}

type uploadKeys struct {
	count string
	block string
	isIP  bool
}

// AssertAllowed rejects uploads while a user or IP block is active, then counts
// the attempt against both.
func (l *UploadLimiter) AssertAllowed(ctx context.Context, userID, ip string) error {
	retryAfter := time.Duration(0)
	for _, pair := range l.keys(userID, ip) {
		_, found, err := l.client.Get(ctx, pair.block)
		if err != nil {
			return fmt.Errorf("me rate limit: %w", err)
		}
		if !found {
			continue
		}
		ttl, err := l.client.TTL(ctx, pair.block)
		if err != nil {
			return fmt.Errorf("me rate limit: %w", err)
		}
		if ttl <= 0 {
			ttl = l.blockFor(pair)
		}
		if ttl > retryAfter {
			retryAfter = ttl
		}
	}
	if retryAfter > 0 {
		return &httpx.ProblemError{
			Status:            http.StatusTooManyRequests,
			Code:              httpx.CodeRateLimited,
			Detail:            "Too many profile image upload requests. Try again later.",
			RetryAfterSeconds: int(retryAfter.Round(time.Second).Seconds()),
		}
	}
	for _, pair := range l.keys(userID, ip) {
		config := l.user
		if pair.isIP {
			config = l.ip
		}
		if err := l.bump(ctx, pair, config); err != nil {
			return err
		}
	}
	return nil
}

func (l *UploadLimiter) bump(ctx context.Context, pair uploadKeys, config UploadLimitConfig) error {
	count, err := l.client.Incr(ctx, pair.count)
	if err != nil {
		return fmt.Errorf("me rate limit: %w", err)
	}
	if count == 1 {
		if err := l.client.Expire(ctx, pair.count, config.Window); err != nil {
			return fmt.Errorf("me rate limit: %w", err)
		}
	}
	if count >= int64(config.MaxAttempts) {
		if err := l.client.Set(ctx, pair.block, "1", config.Block); err != nil {
			return fmt.Errorf("me rate limit: %w", err)
		}
	}
	return nil
}

func (l *UploadLimiter) blockFor(pair uploadKeys) time.Duration {
	if pair.isIP {
		return l.ip.Block
	}
	return l.user.Block
}

func (l *UploadLimiter) keys(userID, ip string) []uploadKeys {
	keys := []uploadKeys{{
		count: redis.Key("users", "profile-image", "upload", "user", userID, "requests"),
		block: redis.Key("users", "profile-image", "upload", "user", userID, "blocked"),
	}}
	if ip != "" {
		ipHash := hashUploadKey(ip)
		keys = append(keys, uploadKeys{
			count: redis.Key("users", "profile-image", "upload", "ip", ipHash, "requests"),
			block: redis.Key("users", "profile-image", "upload", "ip", ipHash, "blocked"),
			isIP:  true,
		})
	}
	return keys
}

func hashUploadKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
