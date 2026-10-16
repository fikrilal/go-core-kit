package app

import (
	"context"
	"fmt"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
)

// ReminderLead is how long before the due instant the reminder goes out; the
// reference's exact offset is unresolved, so this kit owns the value.
const ReminderLead = 72 * time.Hour

// AccountDeletionScheduler arms and cancels the deletion workflow's jobs.
type AccountDeletionScheduler struct {
	scheduler ports.JobScheduler
	clock     clock.Clock
	emailOn   bool
}

// NewAccountDeletionScheduler builds the scheduler seam.
func NewAccountDeletionScheduler(scheduler ports.JobScheduler, clk clock.Clock, emailOn bool) *AccountDeletionScheduler {
	return newDeletionScheduler(scheduler, clk, emailOn)
}

// newDeletionScheduler returns nil without a scheduler so callers can skip.
func newDeletionScheduler(scheduler ports.JobScheduler, clk clock.Clock, emailOn bool) *AccountDeletionScheduler {
	if scheduler == nil {
		return nil
	}
	return &AccountDeletionScheduler{scheduler: scheduler, clock: clk, emailOn: emailOn}
}

// requestMeta carries the request id into the scheduled job's envelope.
func requestMeta(traceID string) queue.Meta {
	return queue.Meta{RequestID: traceID, TraceID: traceID}
}

// ScheduleRequest arms the finalize job for the due instant and, when email is
// configured, the request email now and the reminder before the deadline.
func (s *AccountDeletionScheduler) ScheduleRequest(ctx context.Context, userID string, scheduledFor time.Time, meta queue.Meta) error {
	if err := s.scheduler.Schedule(ctx, QueueUsers, JobFinalizeDeletion, FinalizeTaskID(userID), DeletionJobPayload{UserID: userID}, meta, scheduledFor); err != nil {
		return fmt.Errorf("me account deletion scheduler: ScheduleRequest: %w", err)
	}
	// Emails are best effort: the account state is committed, and a missing
	// email must not fail the request.
	if !s.emailOn {
		return nil
	}
	_ = s.scheduler.Schedule(ctx, email.Queue, JobDeletionRequestedEmail, DeletionEmailTaskID("requested", userID), DeletionJobPayload{UserID: userID}, meta, s.clock.Now())
	if reminderAt := scheduledFor.Add(-ReminderLead); reminderAt.After(s.clock.Now()) {
		_ = s.scheduler.Schedule(ctx, email.Queue, JobDeletionReminderEmail, DeletionEmailTaskID("reminder", userID), DeletionJobPayload{UserID: userID}, meta, reminderAt)
	}
	return nil
}

// CancelRequest removes the pending finalize and reminder tasks.
func (s *AccountDeletionScheduler) CancelRequest(userID string) error {
	if err := s.scheduler.Cancel(QueueUsers, FinalizeTaskID(userID)); err != nil {
		return fmt.Errorf("me account deletion scheduler: CancelRequest: %w", err)
	}
	_ = s.scheduler.Cancel(email.Queue, DeletionEmailTaskID("reminder", userID))
	return nil
}

// ScheduleUploadExpiry arms the stale-upload cleanup, best effort.
func (s *AccountDeletionScheduler) ScheduleUploadExpiry(ctx context.Context, userID, fileID string, at time.Time, meta queue.Meta) {
	_ = s.scheduler.Schedule(ctx, QueueUsers, JobExpireUpload, ExpireTaskID(fileID), FileJobPayload{OwnerUserID: userID, FileID: fileID}, meta, at)
}
