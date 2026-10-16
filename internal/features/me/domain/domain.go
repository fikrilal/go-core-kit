// Package domain holds the me feature's framework-free types.
package domain

import "time"

// Roles.
const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"
)

// User statuses.
const (
	StatusActive    = "ACTIVE"
	StatusSuspended = "SUSPENDED"
	StatusDeleted   = "DELETED"
)

// Auth methods.
const (
	AuthMethodPassword = "PASSWORD"
	AuthMethodGoogle   = "GOOGLE"
)

// Push platforms.
const (
	PushPlatformAndroid = "ANDROID"
	PushPlatformIOS     = "IOS"
	PushPlatformWeb     = "WEB"
)

// User is the account core the Me view carries.
type User struct {
	ID            string
	Email         string
	EmailVerified bool
	Role          string
	Status        string
}

// Profile is the display profile attached to a user.
type Profile struct {
	DisplayName string
	GivenName   string
	FamilyName  string
}

// MeView is the composed Me response core.
type MeView struct {
	User            User
	Profile         Profile
	AuthMethods     []string
	AccountDeletion *AccountDeletion
}

// AccountDeletion is a scheduled account deletion.
type AccountDeletion struct {
	RequestedAt  time.Time
	ScheduledFor time.Time
}

// DeletionKind reports how an account deletion request or cancel ended.
type DeletionKind string

// Account deletion outcomes.
const (
	DeletionOK               DeletionKind = "ok"
	DeletionAlreadyRequested DeletionKind = "already_requested"
	DeletionNotRequested     DeletionKind = "not_requested"
	DeletionLastAdmin        DeletionKind = "last_admin"
	DeletionNotFound         DeletionKind = "not_found"
)

// Session statuses.
const (
	SessionActive  = "active"
	SessionRevoked = "revoked"
	SessionExpired = "expired"
)

// Session is the stored session core. RevokedAt is zero when not revoked.
type Session struct {
	ID         string
	DeviceID   string
	DeviceName string
	IP         string
	UserAgent  string
	LastSeenAt time.Time
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RevokedAt  time.Time
}

// SessionView adds the computed status and the current-session flag.
type SessionView struct {
	Session
	Current bool
	Status  string
}

// FilePurposeProfileImage is the only stored-file purpose today.
const FilePurposeProfileImage = "PROFILE_IMAGE"

// Stored file statuses.
const (
	FileStatusUploading = "UPLOADING"
	FileStatusActive    = "ACTIVE"
	FileStatusDeleted   = "DELETED"
)

// StoredFile is the stored-file core for profile images.
type StoredFile struct {
	ID          string
	Status      string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	Purpose     string
}

// Field is a patch field that can be absent, cleared (null), or set.
type Field struct {
	Present bool
	Null    bool
	Value   string
}

// ProfilePatch carries the profile fields a patch request defined.
type ProfilePatch struct {
	DisplayName Field
	GivenName   Field
	FamilyName  Field
}

// Defined reports whether the patch carries at least one profile field.
func (p ProfilePatch) Defined() bool {
	return p.DisplayName.Present || p.GivenName.Present || p.FamilyName.Present
}

// Push job outcomes and skip reasons mirror the reference's result shape.
const (
	PushOutcomeSent    = "sent"
	PushOutcomeSkipped = "skipped"

	PushReasonSessionNotFound = "session_not_found"
	PushReasonSessionRevoked  = "session_revoked"
	PushReasonSessionExpired  = "session_expired"
	PushReasonUserInactive    = "user_inactive"
	PushReasonNoToken         = "no_token"
	PushReasonInvalidToken    = "invalid_token"
)

// PushSession is the session state a push job reads.
type PushSession struct {
	ID         string
	PushToken  string
	Revoked    bool
	ExpiresAt  time.Time
	UserStatus string
}

// EmailSent is the outcome of a delivered email.
const EmailSent = "sent"

// FinalizeKind is what the finalize transaction did.
type FinalizeKind string

// Finalize transaction results.
const (
	FinalizeDone             FinalizeKind = "finalized"
	FinalizeBlockedLastAdmin FinalizeKind = "blocked_last_admin"
)

// Account deletion finalize outcomes.
const (
	DeletionFinalized        = "finalized"
	DeletionNotDue           = "not_due"
	DeletionBlockedLastAdmin = "blocked_last_admin"

	DeletionOutcomeSkipped       = "skipped"
	DeletionReasonUserNotFound   = "user_not_found"
	DeletionReasonAlreadyDeleted = "already_deleted"
	DeletionReasonNotScheduled   = "not_scheduled"
	DeletionReasonTooLate        = "too_late"
)

// Stored-file cleanup outcomes.
const (
	FileCleanupDeleted = "deleted"
	FileCleanupExpired = "expired"
	FileCleanupSkipped = "skipped"

	FileReasonNotFound     = "file_not_found"
	FileReasonWrongPurpose = "wrong_purpose"
	FileReasonNotUploading = "not_uploading"
)

// UserDeletionEmailState is the account core the deletion emails read.
type UserDeletionEmailState struct {
	ID                   string
	Email                string
	Status               string
	DeletionScheduledFor time.Time
}

// UserFinalizeState is the account core the finalize job reads.
type UserFinalizeState struct {
	ID                   string
	Email                string
	Role                 string
	Status               string
	DeletionScheduledFor time.Time
	RequestedSessionID   string
	RequestedTraceID     string
	DeletedAt            time.Time
}
