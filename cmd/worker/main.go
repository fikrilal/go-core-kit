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
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth"
	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/me"
	meapp "github.com/fikrilal/go-core-kit/internal/features/me/app"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
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
	queueServer, err := queue.NewServer(options, map[string]int{
		queue.DefaultQueue: 1,
		email.Queue:        1,
		push.Queue:         1,
		meapp.QueueUsers:   2,
	}, queue.DefaultConcurrency, logger)
	if err != nil {
		return fmt.Errorf("queue server: %w", err)
	}
	mux, err := queue.NewServeMux(jobHandlers(cfg, database, logger))
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

// jobHandlers registers the feature job handlers this worker consumes. Email
// jobs register when Resend is configured, push jobs when FCM is; skipped
// accounts and sessions are successful outcomes, logged with the ids.
func jobHandlers(cfg config.Config, database *db.Database, logger *slog.Logger) map[string]queue.HandlerFunc {
	queueOptions, err := queue.Options(cfg.RedisURL)
	if err != nil {
		logger.Error("parse REDIS_URL", slog.String("error", err.Error()))
		return map[string]queue.HandlerFunc{}
	}
	return composeHandlers(cfg, database, logger,
		email.New(email.Options{APIKey: cfg.ResendAPIKey, From: cfg.EmailFrom, ReplyTo: cfg.EmailReplyTo}),
		push.New(push.Options{ProjectID: cfg.FCMProjectID, Credentials: cfg.FCMServiceAccount}, clock.System{}),
		storage.New(storage.Options{
			Endpoint:        cfg.StorageS3Endpoint,
			Region:          cfg.StorageS3Region,
			Bucket:          cfg.StorageS3Bucket,
			AccessKeyID:     cfg.StorageS3AccessKeyID,
			SecretAccessKey: cfg.StorageS3SecretAccessKey,
			ForcePathStyle:  cfg.StorageS3ForcePathStyle,
		}, clock.System{}),
		queue.NewScheduler(queueOptions, clock.System{}),
	)
}

// composeHandlers binds the feature handlers to the queue; tests inject
// stub senders to execute the closures.
func composeHandlers(cfg config.Config, database *db.Database, logger *slog.Logger, sender email.Sender, pushSender push.Sender, objectStorage *storage.Client, scheduler *queue.Scheduler) map[string]queue.HandlerFunc {
	handlers := map[string]queue.HandlerFunc{}
	if sender.Enabled() {
		jobs := auth.NewEmailJobs(auth.EmailJobsConfig{
			VerificationTTL: time.Duration(cfg.AuthEmailVerificationTTLSeconds) * time.Second,
			ResetTTL:        time.Duration(cfg.AuthPasswordResetTTLSeconds) * time.Second,
			PublicAppURL:    cfg.PublicAppURL,
		}, auth.EmailJobDeps{Database: database, Email: sender, Clock: clock.System{}})
		handlers[auth.EmailVerificationJob] = queue.Bind(func(ctx context.Context, payload auth.EmailJobPayload, meta queue.Meta) error {
			outcome, err := jobs.SendVerificationEmail(ctx, payload.UserID)
			if err != nil {
				return fmt.Errorf("verification email job: %w", err)
			}
			logEmailOutcome(logger, "verification email job", payload.UserID, outcome.Outcome, outcome.EmailID, meta)
			return nil
		})
		handlers[auth.EmailPasswordResetJob] = queue.Bind(func(ctx context.Context, payload auth.EmailJobPayload, meta queue.Meta) error {
			outcome, err := jobs.SendPasswordResetEmail(ctx, payload.UserID)
			if err != nil {
				return fmt.Errorf("password reset email job: %w", err)
			}
			logEmailOutcome(logger, "password reset email job", payload.UserID, outcome.Outcome, outcome.EmailID, meta)
			return nil
		})
	} else {
		logger.Warn("email is not configured; email jobs are not registered")
	}

	if pushSender.Enabled() {
		jobs := me.NewPushJobs(me.PushJobDeps{Database: database, Push: pushSender, Clock: clock.System{}})
		handlers[push.JobSend] = queue.Bind(func(ctx context.Context, payload push.Payload, meta queue.Meta) error {
			outcome, err := jobs.Send(ctx, payload.SessionID, payload.Notification, payload.Data)
			if err != nil {
				return fmt.Errorf("push job: %w", err)
			}
			logger.Info("push job",
				slog.String("session_id", payload.SessionID),
				slog.String("outcome", outcome.Outcome),
				slog.String("reason", outcome.Reason),
				slog.String("message_id", outcome.MessageID),
				slog.String("provider_code", outcome.ProviderCode),
				slog.String("request_id", meta.RequestID),
				slog.String("trace_id", meta.TraceID),
			)
			return nil
		})
	} else {
		logger.Warn("push is not configured; push jobs are not registered")
	}
	registerAccountDeletionHandlers(handlers, cfg, database, logger, sender, objectStorage, scheduler)
	return handlers
}

// registerAccountDeletionHandlers adds the account-deletion lane: the finalize
// job (always), the stored-file cleanup (with object storage), and the two
// deletion emails (with a configured sender).
func registerAccountDeletionHandlers(handlers map[string]queue.HandlerFunc, cfg config.Config, database *db.Database, logger *slog.Logger, sender email.Sender, objectStorage *storage.Client, scheduler *queue.Scheduler) {
	cleanup := me.NewAccountDeletionJobs(me.AccountDeletionJobDeps{Database: database, Storage: objectStorage, Clock: clock.System{}})
	registerStoredFileHandlers(handlers, logger, cleanup, objectStorage)
	registerFinalizeHandler(handlers, logger, cleanup, scheduler)
	if sender.Enabled() {
		registerDeletionEmailHandlers(handlers, cfg, database, logger, sender)
	}
}

// registerStoredFileHandlers adds the profile-image cleanup jobs.
func registerStoredFileHandlers(handlers map[string]queue.HandlerFunc, logger *slog.Logger, cleanup *meapp.AccountDeletionJobs, objectStorage *storage.Client) {
	if objectStorage == nil || !objectStorage.Enabled() {
		return
	}
	handlers[meapp.JobDeleteStoredFile] = queue.Bind(func(ctx context.Context, payload meapp.FileJobPayload, meta queue.Meta) error {
		outcome, err := cleanup.DeleteStoredFile(ctx, payload.OwnerUserID, payload.FileID)
		if err != nil {
			return fmt.Errorf("worker: cleanup stored file: %w", err)
		}
		logCleanupOutcome(logger, "profile image cleanup job", outcome, meta)
		return nil
	})
	handlers[meapp.JobExpireUpload] = queue.Bind(func(ctx context.Context, payload meapp.FileJobPayload, meta queue.Meta) error {
		outcome, err := cleanup.ExpireUpload(ctx, payload.OwnerUserID, payload.FileID)
		if err != nil {
			return fmt.Errorf("worker: expire upload: %w", err)
		}
		logCleanupOutcome(logger, "profile image expiry job", outcome, meta)
		return nil
	})
}

// registerFinalizeHandler adds the finalize job, which re-arms itself when the
// account is not due yet or is still the last active admin.
func registerFinalizeHandler(handlers map[string]queue.HandlerFunc, logger *slog.Logger, cleanup *meapp.AccountDeletionJobs, scheduler *queue.Scheduler) {
	handlers[meapp.JobFinalizeDeletion] = queue.Bind(func(ctx context.Context, payload meapp.DeletionJobPayload, meta queue.Meta) error {
		outcome, err := cleanup.FinalizeAccountDeletion(ctx, payload.UserID)
		if err != nil {
			return fmt.Errorf("worker: finalize account deletion: %w", err)
		}
		switch outcome.Kind {
		case domain.DeletionNotDue:
			// The account is not due yet: re-arm for the stored instant instead
			// of polling.
			if scheduler != nil {
				if err := scheduler.Schedule(ctx, meapp.QueueUsers, meapp.JobFinalizeDeletion, meapp.FinalizeTaskID(payload.UserID), payload, meta, outcome.ScheduledFor); err != nil {
					return fmt.Errorf("worker: re-delay finalize: %w", err)
				}
			}
		case domain.DeletionBlockedLastAdmin:
			// The account is the last active admin: re-check after a day.
			if scheduler != nil {
				if err := scheduler.Schedule(ctx, meapp.QueueUsers, meapp.JobFinalizeDeletion, meapp.FinalizeTaskID(payload.UserID), payload, meta, clock.System{}.Now().Add(24*time.Hour)); err != nil {
					return fmt.Errorf("worker: re-delay blocked finalize: %w", err)
				}
			}
		case domain.DeletionFinalized, domain.DeletionOutcomeSkipped:
		}
		logDeletionOutcome(logger, payload.UserID, outcome, meta)
		return nil
	})
}

// registerDeletionEmailHandlers adds the two account-deletion emails.
func registerDeletionEmailHandlers(handlers map[string]queue.HandlerFunc, cfg config.Config, database *db.Database, logger *slog.Logger, sender email.Sender) {
	deletionEmails := me.NewDeletionEmails(me.AccountDeletionJobDeps{Database: database, Email: sender, Clock: clock.System{}}, cfg.PublicAppURL)
	handlers[meapp.JobDeletionRequestedEmail] = queue.Bind(func(ctx context.Context, payload meapp.DeletionJobPayload, meta queue.Meta) error {
		outcome, err := deletionEmails.SendRequested(ctx, payload.UserID)
		if err != nil {
			return fmt.Errorf("worker: send deletion request email: %w", err)
		}
		logEmailOutcome(logger, "account deletion requested email job", payload.UserID, outcome.Outcome, outcome.EmailID, meta)
		return nil
	})
	handlers[meapp.JobDeletionReminderEmail] = queue.Bind(func(ctx context.Context, payload meapp.DeletionJobPayload, meta queue.Meta) error {
		outcome, err := deletionEmails.SendReminder(ctx, payload.UserID)
		if err != nil {
			return fmt.Errorf("worker: send deletion reminder email: %w", err)
		}
		logEmailOutcome(logger, "account deletion reminder email job", payload.UserID, outcome.Outcome, outcome.EmailID, meta)
		return nil
	})
}

// logCleanupOutcome logs a cleanup result: the work at info, the skips at warn.
func logCleanupOutcome(logger *slog.Logger, job string, outcome meapp.CleanupOutcome, meta queue.Meta) {
	attrs := []any{
		slog.String("outcome", outcome.Kind),
		slog.String("reason", outcome.Reason),
		slog.String("request_id", meta.RequestID),
		slog.String("trace_id", meta.TraceID),
	}
	if outcome.Kind == domain.FileCleanupSkipped {
		logger.Warn(job, attrs...)
		return
	}
	logger.Info(job, attrs...)
}

// logDeletionOutcome logs a finalize result: a finalize at info, everything
// else (skips, not due, blocked) at warn.
func logDeletionOutcome(logger *slog.Logger, userID string, outcome meapp.FinalizeOutcome, meta queue.Meta) {
	attrs := []any{
		slog.String("user_id", userID),
		slog.String("outcome", outcome.Kind),
		slog.String("reason", outcome.Reason),
		slog.String("request_id", meta.RequestID),
		slog.String("trace_id", meta.TraceID),
	}
	if outcome.Kind == domain.DeletionFinalized {
		logger.Info("account deletion finalize job", attrs...)
		return
	}
	logger.Warn("account deletion finalize job", attrs...)
}

// logEmailOutcome logs a sent email at info with the provider id and every
// skip at warn, mirroring the reference's levels.
func logEmailOutcome(logger *slog.Logger, job, userID, outcome, emailID string, meta queue.Meta) {
	attrs := []any{
		slog.String("user_id", userID),
		slog.String("outcome", outcome),
		slog.String("request_id", meta.RequestID),
		slog.String("trace_id", meta.TraceID),
	}
	if outcome == app.EmailOutcomeSent {
		logger.Info(job, append(attrs, slog.String("email_id", emailID))...)
		return
	}
	logger.Warn(job, attrs...)
}

func closeAndLog(closer func() error, logger *slog.Logger, name string) {
	if err := closer(); err != nil {
		logger.Error("close "+name, slog.Any("error", err))
	}
}
