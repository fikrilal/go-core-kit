package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func TestContractProbes(t *testing.T) {
	suite := contracttest.New(t)
	client := testClient()

	database := dbtest.Open(t)
	mini := miniredis.RunT(t)
	rdb := redis.MustOpen("redis://" + mini.Addr() + "/0")
	t.Cleanup(func() { _ = rdb.Close() })

	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	server, _, err := newServer(testConfig(t), logger, database, rdb)
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	httpServer := httptest.NewServer(server.Handler)
	t.Cleanup(httpServer.Close)

	for _, path := range []string{"/health", "/ready"} {
		req := mustGet(t, httpServer.URL+path)
		resp := contracttest.Do(t, client, req)
		suite.ValidateResponse(t, req, resp)
		contracttest.AssertRequestID(t, resp)
	}

	provided := "22222222-3333-4444-5555-666666666666"
	req := mustGet(t, httpServer.URL+"/health")
	req.Header.Set("X-Request-Id", provided)
	resp := contracttest.Do(t, client, req)
	if got := resp.Header.Get("X-Request-Id"); got != provided {
		t.Fatalf("X-Request-Id = %q, want provided id %q echoed", got, provided)
	}

	mini.SetError("simulated outage")
	resp = contracttest.Do(t, client, mustGet(t, httpServer.URL+"/ready"))
	if resp.Status != http.StatusServiceUnavailable {
		t.Fatalf("/ready status = %d, want 503", resp.Status)
	}
	var probe struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	}
	if err := json.Unmarshal(resp.Body, &probe); err != nil {
		t.Fatalf("decode /ready body: %v\nbody: %s", err, resp.Body)
	}
	if probe.Status != "unavailable" || probe.Checks["redis"] != "unavailable" {
		t.Fatalf("/ready body = %s, want unavailable redis check", resp.Body)
	}
	suite.AssertErrorCodeDeclared(t, "ready.get", "INTERNAL")

	resp = contracttest.Do(t, client, mustGet(t, httpServer.URL+"/missing"))
	contracttest.AssertProblem(t, resp, "NOT_FOUND")
	contracttest.AssertRequestID(t, resp)
}

func mustGet(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}
