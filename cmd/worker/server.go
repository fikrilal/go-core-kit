//go:build !pg

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/health"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

const (
	shutdownTimeout = 10 * time.Second
	headerTimeout   = 5 * time.Second
)

// newProbeServer serves the worker's liveness and readiness probes on its own
// listener; the API keeps HTTP_PORT.
func newProbeServer(cfg config.Config, logger *slog.Logger, database *db.Database, rdb *redis.Client) *http.Server {
	router := httpx.NewRouter(logger)
	health.New(
		health.Check{Name: "database", Verify: database.Ping},
		health.Check{Name: "redis", Verify: rdb.Ping},
	).Register(router)
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.WorkerHTTPPort),
		Handler:           router,
		ReadHeaderTimeout: headerTimeout,
	}
}

// serve runs the probes and drains on cancel: the queue waits for in-flight
// jobs, then the probe server stops inside the shutdown budget.
func serve(ctx context.Context, httpServer *http.Server, listener net.Listener, queueServer *queue.Server, logger *slog.Logger) error {
	probeErr := make(chan error, 1)
	go func() { probeErr <- httpServer.Serve(listener) }()

	select {
	case err := <-probeErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve probes: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	// WithoutCancel because ctx is already canceled; the drain gets its own budget.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	queueServer.Shutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown probes: %w", err)
	}
	logger.Info("worker stopped")
	return nil
}
