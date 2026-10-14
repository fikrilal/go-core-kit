//go:build !pg

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/features/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func testClient() *http.Client {
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

func waitForStatus(t *testing.T, client *http.Client, url string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url) //nolint:noctx // test probe loop
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("probe %s never reached status %d", url, want)
}

func TestEmailHandlersRegistrations(t *testing.T) {
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	database := db.MustOpen(filepath.Join(t.TempDir(), "worker-handlers.db"))
	t.Cleanup(func() { _ = database.Close() })

	unconfigured := jobHandlers(config.Config{}, database, logger)
	if len(unconfigured) != 0 {
		t.Errorf("handlers = %v without configuration, want none", unconfigured)
	}

	handlers := jobHandlers(config.Config{
		ResendAPIKey: "resend-key",
		EmailFrom:    "auth@example.com",
		PublicAppURL: "https://app.example.com",
		FCMProjectID: "demo", FCMServiceAccount: `{"client_email":"worker@demo.iam"}`,
	}, database, logger)
	for _, job := range []string{auth.EmailVerificationJob, auth.EmailPasswordResetJob, push.JobSend} {
		if handlers[job] == nil {
			t.Errorf("handler for %s is missing", job)
		}
	}
}

type stubEmailSender struct {
	sent []email.Message
}

func (s *stubEmailSender) Enabled() bool { return true }

func (s *stubEmailSender) Send(_ context.Context, message email.Message) (string, error) {
	s.sent = append(s.sent, message)
	return "stub-email-1", nil
}

type stubPushSender struct{}

func (s *stubPushSender) Enabled() bool { return true }

func (s *stubPushSender) SendToToken(context.Context, push.Message) (string, error) {
	return "stub-message-1", nil
}

func TestJobHandlersExecuteThroughBind(t *testing.T) {
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	database := db.MustOpen(filepath.Join(t.TempDir(), "worker-jobs.db"))
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	const stamp = "2026-01-01T00:00:00.000000000Z"
	if _, err := database.SQL().ExecContext(t.Context(),
		"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"user-1", "user@example.com", stamp, stamp); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	sender := &stubEmailSender{}
	handlers := composeHandlers(config.Config{
		AuthEmailVerificationTTLSeconds: 86400,
		AuthPasswordResetTTLSeconds:     1800,
		PublicAppURL:                    "https://app.example.com",
	}, database, logger, sender, &stubPushSender{})

	payload, err := queue.WithMeta(auth.EmailJobPayload{UserID: "user-1"}, queue.Meta{RequestID: "req-1", TraceID: "trace-1"})
	if err != nil {
		t.Fatalf("WithMeta() error = %v", err)
	}
	if err := handlers[auth.EmailVerificationJob](t.Context(), asynq.NewTask(auth.EmailVerificationJob, payload)); err != nil {
		t.Fatalf("verification handler error = %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("emails = %d, want the composed closure to send", len(sender.sent))
	}
	var tokens int
	if err := database.SQL().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM email_verification_tokens").Scan(&tokens); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokens != 1 {
		t.Errorf("tokens = %d, want the real repository to mint one", tokens)
	}

	resetPayload, err := queue.WithMeta(auth.EmailJobPayload{UserID: "user-1"}, queue.Meta{})
	if err != nil {
		t.Fatalf("WithMeta(reset) error = %v", err)
	}
	if err := handlers[auth.EmailPasswordResetJob](t.Context(), asynq.NewTask(auth.EmailPasswordResetJob, resetPayload)); err != nil {
		t.Fatalf("reset handler error = %v", err)
	}
	var resetTokens int
	if err := database.SQL().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM password_reset_tokens").Scan(&resetTokens); err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if resetTokens != 1 {
		t.Errorf("reset tokens = %d, want the real repository to mint one", resetTokens)
	}

	pushPayload, err := queue.WithMeta(push.Payload{SessionID: "missing-session"}, queue.Meta{})
	if err != nil {
		t.Fatalf("WithMeta(push) error = %v", err)
	}
	if err := handlers[push.JobSend](t.Context(), asynq.NewTask(push.JobSend, pushPayload)); err != nil {
		t.Fatalf("push handler error = %v, want a successful skip", err)
	}
}

func TestWorkerServesProbesAndShutsDown(t *testing.T) {
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	client := testClient()

	mini := miniredis.RunT(t)
	rdb := redis.MustOpen("redis://" + mini.Addr() + "/0")
	database := db.MustOpen(filepath.Join(t.TempDir(), "worker.db"))
	t.Cleanup(func() {
		_ = database.Close()
		_ = rdb.Close()
	})

	options, err := queue.Options("redis://" + mini.Addr() + "/0")
	if err != nil {
		t.Fatalf("queue.Options() error = %v", err)
	}
	queueServer, err := queue.NewServer(options, map[string]int{queue.DefaultQueue: 1}, 2, logger)
	if err != nil {
		t.Fatalf("queue.NewServer() error = %v", err)
	}
	mux, err := queue.NewServeMux(nil)
	if err != nil {
		t.Fatalf("queue.NewServeMux() error = %v", err)
	}
	httpServer := newProbeServer(config.Config{WorkerHTTPPort: 8081}, logger, database, rdb)

	lc := net.ListenConfig{}
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		if err := queueServer.Start(mux); err != nil {
			done <- err
			return
		}
		done <- serve(ctx, httpServer, listener, queueServer, logger)
	}()

	baseURL := "http://" + listener.Addr().String()
	waitForStatus(t, client, baseURL+"/health", http.StatusOK)
	waitForStatus(t, client, baseURL+"/ready", http.StatusOK)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker run error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not stop after cancel")
	}
}
