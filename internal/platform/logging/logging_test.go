package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
)

func testLogger(buf *bytes.Buffer, format config.LogFormat, level config.LogLevel) *slog.Logger {
	return logging.New(level, format, buf)
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("decode log output: %v\noutput: %s", err, buf.String())
	}
	return entry
}

func TestNewWritesJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf, config.LogFormatJSON, config.LogLevelDebug)

	logger.Info("hello", "user_id", "u1")

	entry := decode(t, &buf)
	if entry["msg"] != "hello" {
		t.Errorf("msg = %v, want hello", entry["msg"])
	}
	if entry["user_id"] != "u1" {
		t.Errorf("user_id = %v, want u1", entry["user_id"])
	}
}

func TestNewWritesText(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf, config.LogFormatText, config.LogLevelDebug)

	logger.Warn("careful")

	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "careful") {
		t.Errorf("text output = %q, want level=WARN and message", out)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf, config.LogFormatJSON, config.LogLevelWarn)

	logger.Debug("hidden")
	logger.Warn("seen")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Errorf("debug entry should be filtered: %s", out)
	}
	if !strings.Contains(out, "seen") {
		t.Errorf("warn entry missing: %s", out)
	}
}

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf, config.LogFormatJSON, config.LogLevelDebug)

	logger.Info("login",
		"password", "hunter2",
		"Authorization", "Bearer secret-token",
		slog.Group("creds", "access_token", "tok-123"),
		"email", "user@example.com",
	)

	out := buf.String()
	for _, secret := range []string{"hunter2", "Bearer secret-token", "tok-123"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q leaked: %s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("redaction marker missing: %s", out)
	}
	if !strings.Contains(out, "user@example.com") {
		t.Errorf("non-sensitive value was redacted: %s", out)
	}
}

func TestRequestBoundFields(t *testing.T) {
	var buf bytes.Buffer
	logger := testLogger(&buf, config.LogFormatJSON, config.LogLevelDebug)
	bound := logger.With(logging.FieldRequestID, "req-1", logging.FieldTraceID, "trace-1")

	bound.Info("ok")

	entry := decode(t, &buf)
	if entry[logging.FieldRequestID] != "req-1" {
		t.Errorf("request_id = %v, want req-1", entry[logging.FieldRequestID])
	}
	if entry[logging.FieldTraceID] != "trace-1" {
		t.Errorf("trace_id = %v, want trace-1", entry[logging.FieldTraceID])
	}
}
