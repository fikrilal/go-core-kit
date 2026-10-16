package app

import (
	"context"
	"fmt"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
)

// Queue and job names for the me feature's worker lane.
const (
	QueueUsers = "users"

	JobFinalizeDeletion       = "users.finalizeAccountDeletion"
	JobDeleteStoredFile       = "users.profileImage.deleteStoredFile"
	JobExpireUpload           = "users.profileImage.expireUpload"
	JobDeletionRequestedEmail = "users.sendAccountDeletionRequestedEmail"
	JobDeletionReminderEmail  = "users.sendAccountDeletionReminderEmail"
)

// DeletionJobPayload is the finalize and deletion-email payload.
type DeletionJobPayload struct {
	UserID string `json:"userId"`
}

// FileJobPayload is the stored-file job payload.
type FileJobPayload struct {
	OwnerUserID string `json:"ownerUserId"`
	FileID      string `json:"fileId"`
}

// FinalizeTaskID is the deterministic finalize task id for a user.
func FinalizeTaskID(userID string) string { return "finalize:" + userID }

// ExpireTaskID is the deterministic expiry task id for a stored file.
func ExpireTaskID(fileID string) string { return "expire:" + fileID }

// DeletionEmailTaskID is the deterministic task id for a deletion email.
func DeletionEmailTaskID(kind, userID string) string { return kind + "-email:" + userID }

// FinalizeOutcome reports one finalize run.
type FinalizeOutcome struct {
	Kind         string
	Reason       string
	ScheduledFor time.Time
	DeletedAt    time.Time
}

// CleanupOutcome reports one stored-file cleanup run.
type CleanupOutcome struct {
	Kind   string
	Reason string
}

// AccountDeletionJobs implements the account-deletion worker lane.
type AccountDeletionJobs struct {
	repository ports.Repository
	storage    ports.ProfileImageStorage
	clock      clock.Clock
}

// NewAccountDeletionJobs builds the lane.
func NewAccountDeletionJobs(repository ports.Repository, storage ports.ProfileImageStorage, clk clock.Clock) *AccountDeletionJobs {
	return &AccountDeletionJobs{repository: repository, storage: storage, clock: clk}
}

// FinalizeAccountDeletion de-identifies a due account, or reports why it could
// not: a not-due run carries its instant so the worker can re-delay.
func (j *AccountDeletionJobs) FinalizeAccountDeletion(ctx context.Context, userID string) (FinalizeOutcome, error) {
	user, found, err := j.repository.UserForFinalize(ctx, userID)
	if err != nil {
		return FinalizeOutcome{}, fmt.Errorf("me account deletion job: UserForFinalize: %w", err)
	}
	if !found {
		return FinalizeOutcome{Kind: domain.DeletionOutcomeSkipped, Reason: domain.DeletionReasonUserNotFound}, nil
	}
	if user.Status == domain.StatusDeleted || !user.DeletedAt.IsZero() {
		return FinalizeOutcome{Kind: domain.DeletionOutcomeSkipped, Reason: domain.DeletionReasonAlreadyDeleted}, nil
	}
	if user.DeletionScheduledFor.IsZero() {
		return FinalizeOutcome{Kind: domain.DeletionOutcomeSkipped, Reason: domain.DeletionReasonNotScheduled}, nil
	}
	now := j.clock.Now()
	if user.DeletionScheduledFor.After(now) {
		return FinalizeOutcome{Kind: domain.DeletionNotDue, ScheduledFor: user.DeletionScheduledFor}, nil
	}
	// The audit row needs the requesting session and trace; 4.5 records both on
	// the user, so a missing one means a broken invariant, not a skip.
	sessionID, traceID := user.RequestedSessionID, user.RequestedTraceID
	if sessionID == "" || traceID == "" {
		return FinalizeOutcome{}, fmt.Errorf("me account deletion job: user %s has no requesting session or trace", userID)
	}
	// The guard and the write share one transaction, so the count cannot go
	// stale between them.
	kind, err := j.repository.FinalizeDeletion(ctx, ports.FinalizeDeletionInput{
		UserID:         userID,
		ScrubbedEmail:  ScrubbedDeletionEmail(userID),
		ActorSessionID: sessionID,
		TraceID:        traceID,
		Now:            now,
	})
	if err != nil {
		return FinalizeOutcome{}, fmt.Errorf("me account deletion job: FinalizeDeletion: %w", err)
	}
	switch kind {
	case domain.FinalizeDone:
		return FinalizeOutcome{Kind: domain.DeletionFinalized, DeletedAt: now}, nil
	case domain.FinalizeBlockedLastAdmin:
		return FinalizeOutcome{Kind: domain.DeletionBlockedLastAdmin}, nil
	}
	return FinalizeOutcome{}, fmt.Errorf("me account deletion job: unexpected finalize result %q", kind)
}

// ScrubbedDeletionEmail is the reserved address a finalized account keeps.
func ScrubbedDeletionEmail(userID string) string {
	return "deleted+" + userID + "@example.invalid"
}

// DeleteStoredFile removes one stored object and marks the record deleted.
func (j *AccountDeletionJobs) DeleteStoredFile(ctx context.Context, ownerUserID, fileID string) (CleanupOutcome, error) {
	file, found, err := j.repository.StoredFileForCleanup(ctx, fileID, ownerUserID)
	if err != nil {
		return CleanupOutcome{}, fmt.Errorf("me account deletion job: StoredFileForCleanup: %w", err)
	}
	if !found {
		return CleanupOutcome{Kind: domain.FileCleanupSkipped, Reason: domain.FileReasonNotFound}, nil
	}
	if file.Purpose != domain.FilePurposeProfileImage {
		return CleanupOutcome{Kind: domain.FileCleanupSkipped, Reason: domain.FileReasonWrongPurpose}, nil
	}
	if file.Status == domain.FileStatusDeleted {
		return CleanupOutcome{Kind: domain.FileCleanupSkipped, Reason: domain.DeletionReasonAlreadyDeleted}, nil
	}
	// The object may already be gone: the delete is best effort, and the record
	// is marked deleted regardless so the job does not loop.
	_ = j.storage.Delete(ctx, file.ObjectKey)
	if err := j.repository.MarkStoredFileDeleted(ctx, fileID, ownerUserID, j.clock.Now()); err != nil {
		return CleanupOutcome{}, fmt.Errorf("me account deletion job: DeleteStoredFile: %w", err)
	}
	return CleanupOutcome{Kind: domain.FileCleanupDeleted}, nil
}

// ExpireUpload marks a stale upload deleted and drops its object.
func (j *AccountDeletionJobs) ExpireUpload(ctx context.Context, ownerUserID, fileID string) (CleanupOutcome, error) {
	file, found, err := j.repository.StoredFileForCleanup(ctx, fileID, ownerUserID)
	if err != nil {
		return CleanupOutcome{}, fmt.Errorf("me account deletion job: StoredFileForCleanup: %w", err)
	}
	if !found {
		return CleanupOutcome{Kind: domain.FileCleanupSkipped, Reason: domain.FileReasonNotFound}, nil
	}
	expired, err := j.repository.ExpireStoredFileIfUploading(ctx, fileID, ownerUserID, j.clock.Now())
	if err != nil {
		return CleanupOutcome{}, fmt.Errorf("me account deletion job: ExpireStoredFileIfUploading: %w", err)
	}
	if !expired {
		return CleanupOutcome{Kind: domain.FileCleanupSkipped, Reason: domain.FileReasonNotUploading}, nil
	}
	_ = j.storage.Delete(ctx, file.ObjectKey)
	return CleanupOutcome{Kind: domain.FileCleanupExpired}, nil
}

// EmailOutcome reports one deletion email run.
type EmailOutcome struct {
	Outcome string
	Reason  string
	EmailID string
}

// DeletionEmails implements the two account-deletion emails.
type DeletionEmails struct {
	repository   ports.Repository
	email        email.Sender
	clock        clock.Clock
	publicAppURL string
}

// NewDeletionEmails builds the email lane.
func NewDeletionEmails(repository ports.Repository, sender email.Sender, clk clock.Clock, publicAppURL string) *DeletionEmails {
	return &DeletionEmails{repository: repository, email: sender, clock: clk, publicAppURL: publicAppURL}
}

// SendRequested sends the "deletion requested" email.
func (j *DeletionEmails) SendRequested(ctx context.Context, userID string) (EmailOutcome, error) {
	state, skip, err := j.loadState(ctx, userID)
	if err != nil || skip != "" {
		// The skip path returns the lookup error as-is: a nil error with a skip
		// reason is the normal outcome.
		return EmailOutcome{Outcome: domain.DeletionOutcomeSkipped, Reason: skip}, err
	}
	message := email.DeletionRequestedEmail(state.Email, email.BrandName(j.publicAppURL), utctime.Time{Time: state.DeletionScheduledFor}.Stored(), email.AppURL(j.publicAppURL))
	emailID, err := j.email.Send(ctx, message)
	if err != nil {
		return EmailOutcome{}, fmt.Errorf("me account deletion email: send requested: %w", err)
	}
	return EmailOutcome{Outcome: domain.EmailSent, EmailID: emailID}, nil
}

// SendReminder sends the "deletion reminder" email, and skips once the due
// instant has passed.
func (j *DeletionEmails) SendReminder(ctx context.Context, userID string) (EmailOutcome, error) {
	state, skip, err := j.loadState(ctx, userID)
	if err != nil || skip != "" {
		return EmailOutcome{Outcome: domain.DeletionOutcomeSkipped, Reason: skip}, err
	}
	if !state.DeletionScheduledFor.After(j.clock.Now()) {
		return EmailOutcome{Outcome: domain.DeletionOutcomeSkipped, Reason: domain.DeletionReasonTooLate}, nil
	}
	message := email.DeletionReminderEmail(state.Email, email.BrandName(j.publicAppURL), utctime.Time{Time: state.DeletionScheduledFor}.Stored(), email.AppURL(j.publicAppURL))
	emailID, err := j.email.Send(ctx, message)
	if err != nil {
		return EmailOutcome{}, fmt.Errorf("me account deletion email: send reminder: %w", err)
	}
	return EmailOutcome{Outcome: domain.EmailSent, EmailID: emailID}, nil
}

func (j *DeletionEmails) loadState(ctx context.Context, userID string) (domain.UserDeletionEmailState, string, error) {
	state, found, err := j.repository.UserDeletionEmailState(ctx, userID)
	if err != nil {
		return domain.UserDeletionEmailState{}, "", fmt.Errorf("me account deletion job: UserDeletionEmailState: %w", err)
	}
	if !found {
		return domain.UserDeletionEmailState{}, domain.DeletionReasonUserNotFound, nil
	}
	if state.Status == domain.StatusDeleted {
		return domain.UserDeletionEmailState{}, domain.DeletionReasonAlreadyDeleted, nil
	}
	if state.DeletionScheduledFor.IsZero() {
		return domain.UserDeletionEmailState{}, domain.DeletionReasonNotScheduled, nil
	}
	return state, "", nil
}
