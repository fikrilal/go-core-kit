// Package redis provides the platform Redis client and key conventions.
package redis

import (
	"context"
	"fmt"
	"strings"

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
	return &Client{rdb: goredis.NewClient(opts)}, nil
}

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
