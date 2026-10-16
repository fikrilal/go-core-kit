package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/app"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
)

var dueNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func deletionFixture(t *testing.T) (*app.AccountDeletionJobs, *fakeRepository, *fakeStorage) {
	t.Helper()
	repo := &fakeRepository{}
	storage := &fakeStorage{}
	return app.NewAccountDeletionJobs(repo, storage, testClock{now: dueNow}), repo, storage
}

func TestFinalizeAccountDeletionOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		state      domain.UserFinalizeState
		found      bool
		repoKind   domain.FinalizeKind
		wantKind   string
		wantReason string
	}{
		{name: "missing user skips", wantKind: domain.DeletionOutcomeSkipped, wantReason: domain.DeletionReasonUserNotFound},
		{name: "already deleted skips", found: true, state: domain.UserFinalizeState{Status: domain.StatusDeleted}, wantKind: domain.DeletionOutcomeSkipped, wantReason: domain.DeletionReasonAlreadyDeleted},
		{name: "no schedule skips", found: true, state: domain.UserFinalizeState{Status: domain.StatusActive}, wantKind: domain.DeletionOutcomeSkipped, wantReason: domain.DeletionReasonNotScheduled},
		{
			name: "future schedule re-delays", found: true,
			state:    domain.UserFinalizeState{Status: domain.StatusActive, DeletionScheduledFor: dueNow.Add(time.Hour)},
			wantKind: domain.DeletionNotDue,
		},
		{
			name: "last admin blocks", found: true, repoKind: domain.FinalizeBlockedLastAdmin,
			state:    domain.UserFinalizeState{Status: domain.StatusActive, Role: domain.RoleAdmin, DeletionScheduledFor: dueNow, RequestedSessionID: "s-1", RequestedTraceID: "t-1"},
			wantKind: domain.DeletionBlockedLastAdmin,
		},
		{
			name: "due account finalizes", found: true,
			state:    domain.UserFinalizeState{Status: domain.StatusActive, DeletionScheduledFor: dueNow, RequestedSessionID: "s-1", RequestedTraceID: "t-1"},
			wantKind: domain.DeletionFinalized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs, repo, _ := deletionFixture(t)
			repo.finalizeState, repo.finalizeFound, repo.finalizeKind = tt.state, tt.found, tt.repoKind
			outcome, err := jobs.FinalizeAccountDeletion(context.Background(), "user-1")
			if err != nil {
				t.Fatalf("FinalizeAccountDeletion() error = %v", err)
			}
			if outcome.Kind != tt.wantKind || outcome.Reason != tt.wantReason {
				t.Fatalf("outcome = %+v, want %s/%s", outcome, tt.wantKind, tt.wantReason)
			}
			assertFinalizeEffects(t, repo, tt.wantKind)
		})
	}
}

// assertFinalizeEffects checks what the repository saw for the outcome.
func assertFinalizeEffects(t *testing.T, repo *fakeRepository, kind string) {
	t.Helper()
	switch kind {
	case domain.DeletionFinalized:
		if len(repo.finalized) != 1 || !strings.HasPrefix(repo.finalized[0].ScrubbedEmail, "deleted+user-1@") {
			t.Fatalf("finalize input = %+v, want the scrubbed address", repo.finalized)
		}
	case domain.DeletionBlockedLastAdmin:
		// The repository owns the guard; here only the outcome matters, and the
		// real-SQL guard test proves the account survives.
	}
}

func TestFinalizeAccountDeletionRequiresRequestingSession(t *testing.T) {
	jobs, repo, _ := deletionFixture(t)
	repo.finalizeFound = true
	repo.finalizeState = domain.UserFinalizeState{Status: domain.StatusActive, DeletionScheduledFor: dueNow}
	if _, err := jobs.FinalizeAccountDeletion(context.Background(), "user-1"); err == nil {
		t.Fatal("FinalizeAccountDeletion() error = nil, want the invariant failure")
	}
}

func TestStoredFileCleanup(t *testing.T) {
	tests := []struct {
		name       string
		file       domain.StoredFile
		found      bool
		expired    bool
		wantKind   string
		wantReason string
	}{
		{name: "unknown file skips", wantKind: domain.FileCleanupSkipped, wantReason: domain.FileReasonNotFound},
		{name: "wrong purpose skips", found: true, file: domain.StoredFile{Purpose: "OTHER"}, wantKind: domain.FileCleanupSkipped, wantReason: domain.FileReasonWrongPurpose},
		{name: "already deleted skips", found: true, file: domain.StoredFile{Purpose: domain.FilePurposeProfileImage, Status: domain.FileStatusDeleted}, wantKind: domain.FileCleanupSkipped, wantReason: domain.DeletionReasonAlreadyDeleted},
		{name: "active file is deleted", found: true, file: domain.StoredFile{ObjectKey: "users/u/p.jpg", Purpose: domain.FilePurposeProfileImage, Status: domain.FileStatusActive}, wantKind: domain.FileCleanupDeleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs, repo, storage := deletionFixture(t)
			repo.cleanupFile, repo.cleanupFound = tt.file, tt.found
			outcome, err := jobs.DeleteStoredFile(context.Background(), "user-1", "file-1")
			if err != nil {
				t.Fatalf("DeleteStoredFile() error = %v", err)
			}
			if outcome.Kind != tt.wantKind || outcome.Reason != tt.wantReason {
				t.Fatalf("outcome = %+v, want %s/%s", outcome, tt.wantKind, tt.wantReason)
			}
			if tt.wantKind == domain.FileCleanupDeleted {
				if len(storage.deleted) != 1 || storage.deleted[0] != tt.file.ObjectKey {
					t.Errorf("deleted keys = %v, want the stored object", storage.deleted)
				}
			}
		})
	}
}

func TestExpireUpload(t *testing.T) {
	jobs, repo, _ := deletionFixture(t)
	repo.cleanupFound = true
	repo.uploadExpired = true
	repo.cleanupFile = domain.StoredFile{ObjectKey: "k", Purpose: domain.FilePurposeProfileImage, Status: domain.FileStatusUploading}
	outcome, err := jobs.ExpireUpload(context.Background(), "user-1", "file-1")
	if err != nil {
		t.Fatalf("ExpireUpload() error = %v", err)
	}
	if outcome.Kind != domain.FileCleanupExpired {
		t.Fatalf("outcome = %+v, want expired", outcome)
	}

	repo.uploadExpired = false
	repo.cleanupFile = domain.StoredFile{Purpose: domain.FilePurposeProfileImage, Status: domain.FileStatusActive}
	skipped, err := jobs.ExpireUpload(context.Background(), "user-1", "file-2")
	if err != nil {
		t.Fatalf("ExpireUpload() error = %v", err)
	}
	if skipped.Kind != domain.FileCleanupSkipped || skipped.Reason != domain.FileReasonNotUploading {
		t.Fatalf("outcome = %+v, want the uploading skip", skipped)
	}
}

func TestDeletionEmails(t *testing.T) {
	tests := []struct {
		name       string
		found      bool
		state      domain.UserDeletionEmailState
		reminder   bool
		wantReason string
		wantSent   bool
	}{
		{name: "missing user skips", wantReason: domain.DeletionReasonUserNotFound},
		{name: "deleted user skips", found: true, state: domain.UserDeletionEmailState{Status: domain.StatusDeleted}, wantReason: domain.DeletionReasonAlreadyDeleted},
		{name: "unscheduled skips", found: true, state: domain.UserDeletionEmailState{Status: domain.StatusActive}, wantReason: domain.DeletionReasonNotScheduled},
		{name: "scheduled sends", found: true, state: domain.UserDeletionEmailState{Status: domain.StatusActive, Email: "user@example.com", DeletionScheduledFor: dueNow.Add(72 * time.Hour)}, wantSent: true},
		{name: "late reminder skips", found: true, reminder: true, state: domain.UserDeletionEmailState{Status: domain.StatusActive, Email: "user@example.com", DeletionScheduledFor: dueNow}, wantReason: domain.DeletionReasonTooLate},
		{name: "early reminder sends", found: true, reminder: true, state: domain.UserDeletionEmailState{Status: domain.StatusActive, Email: "user@example.com", DeletionScheduledFor: dueNow.Add(72 * time.Hour)}, wantSent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{emailState: tt.state, emailFound: tt.found}
			sender := &fakeSender{}
			jobs := app.NewDeletionEmails(repo, sender, testClock{now: dueNow}, "https://app.example.com")
			send := jobs.SendRequested
			if tt.reminder {
				send = jobs.SendReminder
			}
			outcome, err := send(context.Background(), "user-1")
			if err != nil {
				t.Fatalf("send() error = %v", err)
			}
			if tt.wantSent {
				if outcome.Outcome != domain.EmailSent || len(sender.sent) != 1 {
					t.Fatalf("outcome = %+v, sent = %d, want one sent email", outcome, len(sender.sent))
				}
				return
			}
			if outcome.Reason != tt.wantReason || len(sender.sent) != 0 {
				t.Fatalf("outcome = %+v, sent = %d, want the %s skip", outcome, len(sender.sent), tt.wantReason)
			}
		})
	}
}

type fakeScheduler struct {
	scheduled []scheduledJob
	cancelled []string
}

type scheduledJob struct {
	queue   string
	job     string
	taskID  string
	at      time.Time
	payload any
	meta    queue.Meta
}

func (f *fakeScheduler) Schedule(_ context.Context, q, job, taskID string, payload any, meta queue.Meta, at time.Time) error {
	f.scheduled = append(f.scheduled, scheduledJob{queue: q, job: job, taskID: taskID, at: at, payload: payload, meta: meta})
	return nil
}

func (f *fakeScheduler) Cancel(q, taskID string) error {
	f.cancelled = append(f.cancelled, q+"/"+taskID)
	return nil
}

func TestAccountDeletionSchedulerArmsAndCancels(t *testing.T) {
	scheduler := &fakeScheduler{}
	arm := app.NewAccountDeletionScheduler(scheduler, testClock{now: dueNow}, true)
	due := dueNow.Add(30 * 24 * time.Hour)

	if err := arm.ScheduleRequest(context.Background(), "user-1", due, queue.Meta{RequestID: "req-1", TraceID: "t-1"}); err != nil {
		t.Fatalf("ScheduleRequest() error = %v", err)
	}
	if len(scheduler.scheduled) != 3 {
		t.Fatalf("scheduled = %+v, want the finalize, request, and reminder jobs", scheduler.scheduled)
	}
	if got := scheduler.scheduled[0]; got.taskID != "finalize:user-1" || got.job != app.JobFinalizeDeletion || !got.at.Equal(due) {
		t.Errorf("finalize job = %+v, want the deterministic id at the due instant", got)
	}
	if got := scheduler.scheduled[1]; got.taskID != "requested-email:user-1" || got.queue != email.Queue {
		t.Errorf("request email = %+v, want it on the emails queue", got)
	}
	if got := scheduler.scheduled[2]; got.taskID != "reminder-email:user-1" || !got.at.Equal(due.Add(-app.ReminderLead)) {
		t.Errorf("reminder = %+v, want it %s before the deadline", got, app.ReminderLead)
	}

	if err := arm.CancelRequest("user-1"); err != nil {
		t.Fatalf("CancelRequest() error = %v", err)
	}
	want := []string{"users/finalize:user-1", email.Queue + "/reminder-email:user-1"}
	if len(scheduler.cancelled) != 2 || scheduler.cancelled[0] != want[0] || scheduler.cancelled[1] != want[1] {
		t.Fatalf("cancelled = %v, want %v", scheduler.cancelled, want)
	}

	plain := app.NewAccountDeletionScheduler(&fakeScheduler{}, testClock{now: dueNow}, false)
	if err := plain.ScheduleRequest(context.Background(), "user-2", due, queue.Meta{}); err != nil {
		t.Fatalf("ScheduleRequest() error = %v", err)
	}
}

// testClock is a fixed clock for the lane's tests.
type testClock struct{ now time.Time }

func (c testClock) Now() time.Time { return c.now }

// fakeSender records the messages the deletion emails hand over.
type fakeSender struct {
	sent []email.Message
}

func (s *fakeSender) Enabled() bool { return true }

func (s *fakeSender) Send(_ context.Context, message email.Message) (string, error) {
	s.sent = append(s.sent, message)
	return "stub-email-1", nil
}
