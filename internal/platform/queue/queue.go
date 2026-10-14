// Package queue wires the asynq job queue: names, payload metadata, the
// producer the API uses, and the server the worker runs. Retry and backoff
// policy lives here so producers never re-declare it.
package queue

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

// Retry policy: five attempts with jittered exponential backoff.
const (
	MaxRetries       = 5
	RetryBaseDelay   = time.Second
	RetryJitterRatio = 0.2
	TaskTimeout      = 30 * time.Second
)

// Queue and worker defaults live here, mirroring the reference's job options.
const (
	// DefaultQueue is the queue the worker consumes until features add theirs.
	DefaultQueue = "default"
	// DefaultConcurrency is the worker's parallel handler count.
	DefaultConcurrency = 10
)

var (
	jobNamePattern   = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*(?:\.[a-z][a-zA-Z0-9]*)+$`)
	queueNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
)

// JobName validates a dot-separated lowerCamel job type, such as
// "user.sendVerificationEmail".
func JobName(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if !jobNamePattern.MatchString(normalized) {
		return "", fmt.Errorf("queue: invalid job name %q", value)
	}
	return normalized, nil
}

// Name validates a lowercase hyphenated queue name.
func Name(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if !queueNamePattern.MatchString(normalized) {
		return "", fmt.Errorf("queue: invalid queue name %q", value)
	}
	return normalized, nil
}

// Meta is the job metadata carried inside the payload so the worker can bind
// the originating request to its logs. OTel's traceparent joins this struct
// when the otel package lands.
type Meta struct {
	RequestID string `json:"request_id,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
}

type payloadWithMeta struct {
	Meta Meta `json:"__meta"`
}

// WithMeta marshals a payload with the metadata envelope prepended.
func WithMeta(payload any, meta Meta) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal payload: %w", err)
	}
	envelope, err := json.Marshal(payloadWithMeta{Meta: meta})
	if err != nil {
		return nil, fmt.Errorf("queue: marshal meta: %w", err)
	}
	// Merge the two objects so the payload keeps its own shape.
	merged := make(map[string]json.RawMessage)
	if err := json.Unmarshal(envelope, &merged); err != nil {
		return nil, fmt.Errorf("queue: merge meta: %w", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &body); err != nil {
		return nil, fmt.Errorf("queue: merge payload: %w", err)
	}
	for key, value := range body {
		if key == "__meta" {
			continue
		}
		merged[key] = value
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal merged payload: %w", err)
	}
	return out, nil
}

// MetaFromTask reads the metadata envelope the producer wrote.
func MetaFromTask(task *asynq.Task) Meta {
	var envelope payloadWithMeta
	if err := json.Unmarshal(task.Payload(), &envelope); err != nil {
		return Meta{}
	}
	return envelope.Meta
}

// Options parses REDIS_URL into asynq client options, mirroring the platform
// redis client: the URL is parsed, not dialed.
func Options(redisURL string) (asynq.RedisClientOpt, error) {
	parsed, err := url.Parse(strings.TrimSpace(redisURL))
	if err != nil {
		return asynq.RedisClientOpt{}, fmt.Errorf("queue: parse redis url: %w", err)
	}
	if parsed.Scheme != "redis" && parsed.Scheme != "rediss" {
		return asynq.RedisClientOpt{}, fmt.Errorf("queue: redis url scheme %q is not redis or rediss", parsed.Scheme)
	}
	options := asynq.RedisClientOpt{Addr: parsed.Host}
	if parsed.User != nil {
		options.Username = parsed.User.Username()
		options.Password, _ = parsed.User.Password()
	}
	if path := strings.TrimPrefix(parsed.Path, "/"); path != "" {
		database, err := strconv.Atoi(path)
		if err != nil {
			return asynq.RedisClientOpt{}, fmt.Errorf("queue: redis url database %q is not a number", path)
		}
		options.DB = database
	}
	if parsed.Scheme == "rediss" {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return options, nil
}
