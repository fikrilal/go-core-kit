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

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
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
