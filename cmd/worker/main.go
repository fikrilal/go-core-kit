//go:build !pg

// Command worker is the background jobs bootstrap.
// This build is SQLite until a pg process exists.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

// os.Exit skips defers, so runMain returns the status.
func main() {
	os.Exit(runMain())
}

func runMain() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker: "+err.Error())
		return 1
	}
	logger := logging.New(cfg.LogLevel, cfg.LogFormat, os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("worker exited with error", slog.Any("error", err))
		return 1
	}
	return 0
}

// run wires the worker. The API owns migrations, Redis Open does not dial, and
// /ready is the connectivity check. No job handlers exist yet; 5.2 adds them.
func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeAndLog(database.Close, logger, "database")

	rdb, err := redis.Open(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("open redis: %w", err)
	}
	defer closeAndLog(rdb.Close, logger, "redis")

	options, err := queue.Options(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("queue options: %w", err)
	}
	queueServer, err := queue.NewServer(options, map[string]int{queue.DefaultQueue: 1}, queue.DefaultConcurrency, logger)
	if err != nil {
		return fmt.Errorf("queue server: %w", err)
	}
	mux, err := queue.NewServeMux(nil)
	if err != nil {
		return fmt.Errorf("queue handlers: %w", err)
	}
	if err := queueServer.Start(mux); err != nil {
		return fmt.Errorf("start queue server: %w", err)
	}

	httpServer := newProbeServer(cfg, logger, database, rdb)
	// A canceled context aborts the bind.
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.Info("worker started",
		slog.String("addr", listener.Addr().String()),
		slog.String("env", string(cfg.AppEnv)),
	)
	return serve(ctx, httpServer, listener, queueServer, logger)
}

func closeAndLog(closer func() error, logger *slog.Logger, name string) {
	if err := closer(); err != nil {
		logger.Error("close "+name, slog.Any("error", err))
	}
}
