package queue_test

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
)

func TestJobAndQueueNames(t *testing.T) {
	if _, err := queue.JobName("user.sendVerificationEmail"); err != nil {
		t.Errorf("JobName(valid) error = %v", err)
	}
	for _, invalid := range []string{"sendEmail", "user..send", "User.Send", "user.send."} {
		if _, err := queue.JobName(invalid); err == nil {
			t.Errorf("JobName(%q) = nil, want an error", invalid)
		}
	}
	if _, err := queue.Name("users"); err != nil {
		t.Errorf("Name(valid) error = %v", err)
	}
	for _, invalid := range []string{"Users", "-users", "users queue", ""} {
		if _, err := queue.Name(invalid); err == nil {
			t.Errorf("Name(%q) = nil, want an error", invalid)
		}
	}
}

func TestWithMetaAndMetaFromTask(t *testing.T) {
	encoded, err := queue.WithMeta(map[string]any{"userId": "user-1", "count": 2}, queue.Meta{RequestID: "req-1", TraceID: "trace-1"})
	if err != nil {
		t.Fatalf("WithMeta() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded["userId"] != "user-1" || decoded["count"] != float64(2) {
		t.Errorf("payload = %v, want the original fields preserved", decoded)
	}
	meta := queue.MetaFromTask(asynq.NewTask("test.echo", encoded))
	if meta.RequestID != "req-1" || meta.TraceID != "trace-1" {
		t.Errorf("meta = %+v, want the envelope values", meta)
	}

	plain := asynq.NewTask("test.echo", []byte(`{"userId":"user-1"}`))
	if meta := queue.MetaFromTask(plain); meta.RequestID != "" || meta.TraceID != "" {
		t.Errorf("meta from a payload without an envelope = %+v, want empty", meta)
	}
}

func TestOptionsParsing(t *testing.T) {
	options, err := queue.Options("redis://user:secret@127.0.0.1:6379/2")
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}
	if options.Addr != "127.0.0.1:6379" || options.Username != "user" || options.Password != "secret" || options.DB != 2 {
		t.Errorf("options = %+v, want the parsed URL parts", options)
	}

	secure, err := queue.Options("rediss://cache.example:6380")
	if err != nil {
		t.Fatalf("Options(rediss) error = %v", err)
	}
	if secure.TLSConfig == nil {
		t.Error("rediss URL produced no TLS config")
	}

	for _, invalid := range []string{"http://127.0.0.1:6379", "redis://127.0.0.1:6379/notanumber"} {
		if _, err := queue.Options(invalid); err == nil {
			t.Errorf("Options(%q) = nil, want an error", invalid)
		}
	}
}

func TestRetryDelayGrowsAndStaysJittered(t *testing.T) {
	sample := func(attempts int) (time.Duration, time.Duration) {
		minimum, maximum := time.Hour, time.Duration(0)
		for range 64 {
			delay := queue.RetryDelay(attempts, nil, nil)
			minimum = min(minimum, delay)
			maximum = max(maximum, delay)
		}
		return minimum, maximum
	}

	for attempts := range queue.MaxRetries {
		lower, upper := sample(attempts)
		base := queue.RetryBaseDelay << attempts
		minBound := time.Duration(float64(base) * (1 - queue.RetryJitterRatio))
		maxBound := time.Duration(float64(base) * (1 + queue.RetryJitterRatio))
		if lower < minBound || upper > maxBound {
			t.Errorf("attempt %d = [%s, %s], want within [%s, %s]", attempts, lower, upper, minBound, maxBound)
		}
		if attempts > 0 {
			previousUpper := func() time.Duration {
				_, previous := sample(attempts - 1)
				return previous
			}()
			if lower <= previousUpper {
				t.Errorf("attempt %d lower bound %s is not above attempt %d upper bound %s", attempts, lower, attempts-1, previousUpper)
			}
		}
	}
}

func TestEnqueueConsumeRoundTrip(t *testing.T) {
	mini := miniredis.RunT(t)
	options, err := queue.Options("redis://" + mini.Addr() + "/0")
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	server, err := queue.NewServer(options, map[string]int{queue.DefaultQueue: 1}, 1, logger)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	type echoPayload struct {
		UserID string `json:"userId"`
	}

	type consumed struct {
		userID string
		meta   queue.Meta
	}
	received := make(chan consumed, 1)
	mux, err := queue.NewServeMux(map[string]queue.HandlerFunc{
		"test.echo": queue.Bind(func(_ context.Context, payload echoPayload, meta queue.Meta) error {
			received <- consumed{userID: payload.UserID, meta: meta}
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewServeMux() error = %v", err)
	}
	if err := server.Start(mux); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(server.Shutdown)

	producer := queue.NewProducer(options)
	t.Cleanup(func() { _ = producer.Close() })
	if err := producer.Enqueue(t.Context(), queue.DefaultQueue, "test.echo",
		map[string]any{"userId": "user-1"}, queue.Meta{RequestID: "req-1", TraceID: "trace-1"}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	select {
	case got := <-received:
		if got.userID != "user-1" {
			t.Errorf("payload userId = %q, want user-1", got.userID)
		}
		if got.meta.RequestID != "req-1" || got.meta.TraceID != "trace-1" {
			t.Errorf("meta = %+v, want the enqueued ids", got.meta)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("job was not consumed")
	}
}

func TestEnqueueRejectsInvalidNames(t *testing.T) {
	mini := miniredis.RunT(t)
	options, err := queue.Options("redis://" + mini.Addr() + "/0")
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}
	producer := queue.NewProducer(options)
	t.Cleanup(func() { _ = producer.Close() })

	if err := producer.Enqueue(t.Context(), queue.DefaultQueue, "notAJob", map[string]any{}, queue.Meta{}); err == nil {
		t.Error("Enqueue() with an invalid job name = nil, want an error")
	}
	if err := producer.Enqueue(t.Context(), "Bad Queue", "test.echo", map[string]any{}, queue.Meta{}); err == nil {
		t.Error("Enqueue() with an invalid queue name = nil, want an error")
	}
}
