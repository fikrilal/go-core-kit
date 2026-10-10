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

	"github.com/fikrilal/go-core-kit/internal/features/auth"
	"github.com/fikrilal/go-core-kit/internal/features/me"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/health"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

const (
	shutdownTimeout = 10 * time.Second
	headerTimeout   = 5 * time.Second
)

func newServer(cfg config.Config, logger *slog.Logger, database *db.Database, rdb *redis.Client) (*http.Server, error) {
	router := httpx.NewRouter(logger, httpx.WithIdempotency(rdb))
	health.New(
		health.Check{Name: "database", Verify: database.Ping},
		health.Check{Name: "redis", Verify: rdb.Ping},
	).Register(router)
	guard, err := auth.Register(router, auth.Deps{
		Database: database,
		Redis:    rdb,
		Clock:    clock.System{},
		Config: auth.Config{
			AccessTokenTTL: time.Duration(cfg.AuthAccessTokenTTLSeconds) * time.Second,
			// SessionTTL is AUTH_REFRESH_TOKEN_TTL_SECONDS. The feature uses it for the session and the refresh token.
			SessionTTL:            time.Duration(cfg.AuthRefreshTokenTTLSeconds) * time.Second,
			PasswordMinLength:     cfg.AuthPasswordMinLength,
			LoginMaxAttempts:      cfg.AuthLoginMaxAttempts,
			LoginWindow:           time.Duration(cfg.AuthLoginWindowSeconds) * time.Second,
			LoginBlock:            time.Duration(cfg.AuthLoginBlockSeconds) * time.Second,
			Issuer:                cfg.AuthIssuer,
			Audience:              cfg.AuthAudience,
			SigningKeysJSON:       cfg.AuthSigningKeysJSON,
			SigningKeysJSONBase64: cfg.AuthSigningKeysJSONBase64,
			ProductionLike:        cfg.ProductionLike(),
			OIDCGoogleClientIDs:   cfg.AuthOIDCGoogleClientIDs,
			OIDCGoogleJWKSURL:     cfg.AuthOIDCGoogleJWKSURL,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("register auth: %w", err)
	}
	me.Register(router, me.Deps{Database: database, Clock: clock.System{}, Guard: guard, PushEnabled: cfg.PushEnabled})
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: headerTimeout,
	}, nil
}

// serve drains on cancel. WithoutCancel is required because ctx is already canceled.
// The header timeout holds Shutdown, so the drain is longer than headerTimeout.
func serve(ctx context.Context, server *http.Server, listener net.Listener, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		logger.Info("api stopped")
		return nil
	}
}
