// Package ports defines the me feature's dependency interfaces.
package ports

import (
	"context"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/push"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
	"github.com/fikrilal/go-core-kit/internal/platform/storage"
)

// UpdateProfileInput carries a profile patch for one user.
type UpdateProfileInput struct {
	UserID  string
	Profile domain.ProfilePatch
	Now     time.Time
}

// SortField is one ordered sort key for a list request.
type SortField struct {
	Field string
	Desc  bool
}

// ListSessionsInput carries a keyset page request for one user.
type ListSessionsInput struct {
	UserID string
	Limit  int
	Sort   []SortField
	Cursor string
}

// SessionPage is one page of stored sessions.
type SessionPage struct {
	Sessions   []domain.Session
	HasMore    bool
	NextCursor string
}

// UpsertPushTokenInput carries a push registration for the caller's session.
type UpsertPushTokenInput struct {
	UserID    string
	SessionID string
	Platform  string
	Token     string
	Now       time.Time
}

// CreateStoredFileInput carries a stored-file record created with a plan.
type CreateStoredFileInput struct {
	ID          string
	OwnerUserID string
	Bucket      string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	TraceID     string
	Now         time.Time
}

// AttachProfileImageInput carries an attach for the caller's profile.
type AttachProfileImageInput struct {
	UserID string
	FileID string
	Now    time.Time
}

// ProfileImageState is the caller's attached profile image pointer.
type ProfileImageState struct {
	FileID    string
	ObjectKey string
}

// RequestAccountDeletionInput carries a deletion request.
type RequestAccountDeletionInput struct {
	UserID       string
	SessionID    string
	TraceID      string
	Now          time.Time
	ScheduledFor time.Time
}

// CancelAccountDeletionInput carries a deletion cancel.
type CancelAccountDeletionInput struct {
	UserID    string
	SessionID string
	TraceID   string
	Now       time.Time
}

// JobScheduler schedules and cancels delayed jobs by deterministic task id.
type JobScheduler interface {
	Schedule(ctx context.Context, queue, job, taskID string, payload any, meta queue.Meta, at time.Time) error
	Cancel(queue, taskID string) error
}

// FinalizeDeletionInput carries the de-identification transaction.
type FinalizeDeletionInput struct {
	UserID         string
	ScrubbedEmail  string
	ActorSessionID string
	TraceID        string
	Now            time.Time
}

// PushSender delivers one push and reports the provider message id.
type PushSender interface {
	SendToToken(ctx context.Context, message push.Message) (string, error)
}

// ProfileImageStorage signs client-side object access and inspects objects.
type ProfileImageStorage interface {
	Enabled() bool
	Bucket() string
	PresignPut(key, contentType string, ttl time.Duration) (storage.PresignedPut, error)
	PresignGet(key string, ttl time.Duration) (storage.PresignedGet, error)
	Head(ctx context.Context, key string) (storage.HeadResult, error)
	Delete(ctx context.Context, key string) error
}

// Repository is the me feature's persistence boundary.
type Repository interface {
	GetMe(ctx context.Context, userID string) (domain.MeView, bool, error)
	UpdateProfile(ctx context.Context, in UpdateProfileInput) (domain.MeView, bool, error)
	FindUserStatus(ctx context.Context, userID string) (string, bool, error)
	ListSessions(ctx context.Context, in ListSessionsInput) (SessionPage, error)
	RevokeSession(ctx context.Context, userID, sessionID string, now time.Time) (bool, error)
	UpsertPushToken(ctx context.Context, in UpsertPushTokenInput) (bool, error)
	RevokePushToken(ctx context.Context, userID, sessionID string, now time.Time) error
	CreateProfileImageFile(ctx context.Context, in CreateStoredFileInput) (bool, error)
	FindStoredFile(ctx context.Context, fileID, userID string) (domain.StoredFile, bool, error)
	MarkStoredFileDeleted(ctx context.Context, fileID, userID string, now time.Time) error
	ProfileImage(ctx context.Context, userID string) (ProfileImageState, bool, error)
	AttachProfileImage(ctx context.Context, in AttachProfileImageInput) (previousObjectKey string, found bool, err error)
	ClearProfileImage(ctx context.Context, userID string, now time.Time) (clearedObjectKey string, found bool, err error)
	RequestAccountDeletion(ctx context.Context, in RequestAccountDeletionInput) (domain.DeletionKind, time.Time, error)
	CancelAccountDeletion(ctx context.Context, in CancelAccountDeletionInput) (domain.DeletionKind, error)
	SessionForPush(ctx context.Context, sessionID string) (domain.PushSession, bool, error)
	ClearSessionPushTokenIfMatches(ctx context.Context, sessionID, token string, now time.Time) error
	UserForFinalize(ctx context.Context, userID string) (domain.UserFinalizeState, bool, error)
	FinalizeDeletion(ctx context.Context, in FinalizeDeletionInput) (domain.FinalizeKind, error)
	StoredFileForCleanup(ctx context.Context, fileID, ownerUserID string) (domain.StoredFile, bool, error)
	ExpireStoredFileIfUploading(ctx context.Context, fileID, ownerUserID string, now time.Time) (bool, error)
	UserDeletionEmailState(ctx context.Context, userID string) (domain.UserDeletionEmailState, bool, error)
}
