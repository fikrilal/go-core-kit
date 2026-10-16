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
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/health"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
)

const (
	shutdownTimeout = 10 * time.Second
	headerTimeout   = 5 * time.Second
)

func newServer(cfg config.Config, logger *slog.Logger, database *db.Database, rdb *redis.Client) (*http.Server, func() error, error) {
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
		return nil, nil, fmt.Errorf("register auth: %w", err)
	}
	objectStorage := storage.New(storage.Options{
		Endpoint:        cfg.StorageS3Endpoint,
		Region:          cfg.StorageS3Region,
		Bucket:          cfg.StorageS3Bucket,
		AccessKeyID:     cfg.StorageS3AccessKeyID,
		SecretAccessKey: cfg.StorageS3SecretAccessKey,
		ForcePathStyle:  cfg.StorageS3ForcePathStyle,
	}, clock.System{})
	// The scheduler arms the deletion workflow's delayed jobs; it lives for the
	// process lifetime. Without REDIS_URL the API's own tests run unscheduled.
	// The interface must stay nil without a URL: a typed nil pointer would pass
	// the feature's nil check and panic on use.
	var scheduler ports.JobScheduler
	var schedulerCloser *queue.Scheduler
	if cfg.RedisURL != "" {
		queueOptions, err := queue.Options(cfg.RedisURL)
		if err != nil {
			return nil, nil, fmt.Errorf("api: parse REDIS_URL: %w", err)
		}
		schedulerCloser = queue.NewScheduler(queueOptions, clock.System{})
		scheduler = schedulerCloser
	}
	// The API owns the scheduler for the process lifetime; the caller closes it.
	closeScheduler := func() error {
		if schedulerCloser == nil {
			return nil
		}
		return schedulerCloser.Close()
	}
	me.Register(router, me.Deps{
		Database:          database,
		Redis:             rdb,
		Clock:             clock.System{},
		Guard:             guard,
		Storage:           objectStorage,
		Scheduler:         scheduler,
		EmailEnabled:      cfg.ResendAPIKey != "" && cfg.EmailFrom != "",
		UploadExpireDelay: time.Duration(cfg.UsersProfileImageUploadExpireDelaySeconds) * time.Second,
		// The push gate follows the FCM credentials, like the reference.
		PushEnabled: push.New(push.Options{ProjectID: cfg.FCMProjectID, Credentials: cfg.FCMServiceAccount}, clock.System{}).Enabled(),
		UploadUserLimit: me.UploadLimitConfig{
			MaxAttempts: cfg.UsersProfileImageUploadUserMaxAttempts,
			Window:      time.Duration(cfg.UsersProfileImageUploadUserWindowSeconds) * time.Second,
			Block:       time.Duration(cfg.UsersProfileImageUploadUserBlockSeconds) * time.Second,
		},
		UploadIPLimit: me.UploadLimitConfig{
			MaxAttempts: cfg.UsersProfileImageUploadIPMaxAttempts,
			Window:      time.Duration(cfg.UsersProfileImageUploadIPWindowSeconds) * time.Second,
			Block:       time.Duration(cfg.UsersProfileImageUploadIPBlockSeconds) * time.Second,
		},
	})
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: headerTimeout,
	}, closeScheduler, nil
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
