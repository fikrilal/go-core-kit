// Package redis provides the platform Redis client and key conventions.
package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client is the platform handle to Redis.
type Client struct {
	rdb *goredis.Client
}

// Open connects to Redis using the configured URL.
func Open(url string) (*Client, error) {
	opts, err := goredis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	goredis.SetLogger(quietLogger{})
	return &Client{rdb: goredis.NewClient(opts)}, nil
}

// quietLogger discards go-redis internal logs; failures surface through wrapped
// errors and readiness checks so structured logging stays one pipeline.
type quietLogger struct{}

func (quietLogger) Printf(_ context.Context, _ string, _ ...any) {}

// MustOpen is Open for composition roots; it panics on invalid configuration.
func MustOpen(url string) *Client {
	client, err := Open(url)
	if err != nil {
		panic(err)
	}
	return client
}

// Ping verifies connectivity for readiness checks.
func (c *Client) Ping(ctx context.Context) error {
	if err := c.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}

// Get returns the value stored under key and whether it exists.
func (c *Client) Get(ctx context.Context, key string) (string, bool, error) {
	value, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get redis key: %w", err)
	}
	return value, true, nil
}

// Set stores value under key with a TTL.
func (c *Client) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.rdb.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("set redis key: %w", err)
	}
	return nil
}

// SetNX stores value under key only when the key does not exist.
func (c *Client) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	stored, err := c.rdb.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("setnx redis key: %w", err)
	}
	return stored, nil
}

// Del deletes the given keys.
func (c *Client) Del(ctx context.Context, keys ...string) error {
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("del redis key: %w", err)
	}
	return nil
}

// Incr increments the counter at key and returns the new value.
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	value, err := c.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("incr redis key: %w", err)
	}
	return value, nil
}

// Expire sets a TTL on key.
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := c.rdb.Expire(ctx, key, ttl).Err(); err != nil {
		return fmt.Errorf("expire redis key: %w", err)
	}
	return nil
}

// TTL reports the remaining TTL of key; missing or persistent keys report 0.
func (c *Client) TTL(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := c.rdb.TTL(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("ttl redis key: %w", err)
	}
	if ttl < 0 {
		return 0, nil
	}
	return ttl, nil
}

// Close releases the connection.
func (c *Client) Close() error {
	if err := c.rdb.Close(); err != nil {
		return fmt.Errorf("close redis: %w", err)
	}
	return nil
}

// Key joins parts into a colon-separated, namespaced Redis key.
func Key(parts ...string) string {
	return strings.Join(parts, ":")
}
