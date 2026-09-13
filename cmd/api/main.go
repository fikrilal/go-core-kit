//go:build !pg

// Command api is the HTTP process bootstrap.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "api: "+err.Error())
		return 1
	}
	logger := logging.New(cfg.LogLevel, cfg.LogFormat, os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("api exited with error", slog.Any("error", err))
		return 1
	}
	return 0
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer closeAndLog(database.Close, logger, "database")

	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	rdb, err := redis.Open(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("open redis: %w", err)
	}
	defer closeAndLog(rdb.Close, logger, "redis")

	httpServer := newServer(cfg.HTTPPort, logger, database, rdb)
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.Info("api started",
		slog.String("addr", listener.Addr().String()),
		slog.String("env", string(cfg.AppEnv)),
	)
	return serve(ctx, httpServer, listener, logger)
}

func closeAndLog(closer func() error, logger *slog.Logger, name string) {
	if err := closer(); err != nil {
		logger.Error("close "+name, slog.Any("error", err))
	}
}
