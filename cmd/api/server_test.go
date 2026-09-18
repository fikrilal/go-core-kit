//go:build !pg

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func TestServeServesProbesAndShutsDown(t *testing.T) {
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	client := testClient()

	mini := miniredis.RunT(t)
	rdb := redis.MustOpen("redis://" + mini.Addr() + "/0")
	database := db.MustOpen(filepath.Join(t.TempDir(), "api.db"))
	t.Cleanup(func() {
		_ = database.Close()
		_ = rdb.Close()
	})
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	httpServer, err := newServer(testConfig(t), logger, database, rdb)
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	lc := net.ListenConfig{}
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, httpServer, listener, logger)
	}()

	baseURL := "http://" + listener.Addr().String()
	waitForStatus(t, client, baseURL+"/health", http.StatusOK)

	assertProbe(t, client, baseURL+"/health", http.StatusOK, "ok")
	assertProbe(t, client, baseURL+"/ready", http.StatusOK, "ok")

	mini.SetError("simulated outage")
	assertProbe(t, client, baseURL+"/ready", http.StatusServiceUnavailable, "unavailable")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() error = %v, want nil after shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not return after shutdown")
	}
}

func testClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{DisableKeepAlives: true},
		Timeout:   5 * time.Second,
	}
}

func waitForStatus(t *testing.T, client *http.Client, url string, wantStatus int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if status, err := probe(t.Context(), client, url); err == nil && status == wantStatus {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server did not reach status %d at %s", wantStatus, url)
}

func assertProbe(t *testing.T, client *http.Client, url string, wantStatus int, wantState string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != wantStatus {
		t.Errorf("%s status = %d, want %d", url, resp.StatusCode, wantStatus)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s body: %v", url, err)
	}
	var probe struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("decode %s body: %v", url, err)
	}
	if probe.Status != wantState {
		t.Errorf("%s status field = %q, want %q", url, probe.Status, wantState)
	}
}

func probe(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("new request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}
