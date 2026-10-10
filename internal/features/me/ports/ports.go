// Package ports defines the me feature's dependency interfaces.
package ports

import (
	"context"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
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

// Repository is the me feature's persistence boundary.
type Repository interface {
	GetMe(ctx context.Context, userID string) (domain.MeView, bool, error)
	UpdateProfile(ctx context.Context, in UpdateProfileInput) (domain.MeView, bool, error)
	FindUserStatus(ctx context.Context, userID string) (string, bool, error)
	ListSessions(ctx context.Context, in ListSessionsInput) (SessionPage, error)
	RevokeSession(ctx context.Context, userID, sessionID string, now time.Time) (bool, error)
	UpsertPushToken(ctx context.Context, in UpsertPushTokenInput) (bool, error)
	RevokePushToken(ctx context.Context, userID, sessionID string, now time.Time) error
}
