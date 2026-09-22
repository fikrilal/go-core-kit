package health_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/health"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
)

type probeResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func testProbes(t *testing.T, checks ...health.Check) (*httpx.Router, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := logging.New(config.LogLevelDebug, config.LogFormatJSON, &logs)
	router := httpx.NewRouter(logger)
	health.New(checks...).Register(router)
	return router, &logs
}

func get(t *testing.T, router *httpx.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return rec
}

func decodeProbe(t *testing.T, rec *httptest.ResponseRecorder) probeResponse {
	t.Helper()
	var body probeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return body
}

func TestHealthReturnsOK(t *testing.T) {
	router, _ := testProbes(t)

	rec := get(t, router, "/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id missing")
	}
	if body := decodeProbe(t, rec); body.Status != "ok" {
		t.Errorf("body = %+v, want status ok", body)
	}
}

func TestReadyWithAllChecksPassing(t *testing.T) {
	okCheck := health.Check{Name: "redis", Verify: func(context.Context) error { return nil }}
	router, _ := testProbes(t, okCheck)

	rec := get(t, router, "/ready")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := decodeProbe(t, rec); body.Status != "ok" {
		t.Errorf("body = %+v, want status ok", body)
	}
}

func TestReadyWithFailingCheck(t *testing.T) {
	downCheck := health.Check{
		Name:   "redis",
		Verify: func(context.Context) error { return errors.New("connection refused") },
	}
	router, logs := testProbes(t, downCheck)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ready", nil)
	req.Header.Set("X-Request-Id", "req-ready-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body := decodeProbe(t, rec)
	if body.Status != "unavailable" || body.Checks["redis"] != "unavailable" {
		t.Errorf("body = %+v, want failing redis", body)
	}
	if strings.Contains(rec.Body.String(), "connection refused") {
		t.Errorf("response leaked check error: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "readiness check failed") || !strings.Contains(logs.String(), "connection refused") {
		t.Errorf("check failure not logged: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "req-ready-1") {
		t.Errorf("warning missing request id: %s", logs.String())
	}
	if strings.Contains(logs.String(), "http request") {
		t.Errorf("probes should skip the access log: %s", logs.String())
	}
}

func TestProbesSkipAccessLog(t *testing.T) {
	router, logs := testProbes(t)

	get(t, router, "/health")
	get(t, router, "/ready")

	if logs.Len() != 0 {
		t.Errorf("access log should be empty for probes, got: %s", logs.String())
	}
}
