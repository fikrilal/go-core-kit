// Package ports defines the auth feature's dependency interfaces.
package ports

import (
	"context"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
)

// CreateUserInput carries a new password registration.
type CreateUserInput struct {
	ID           string
	Email        string
	PasswordHash string
	Now          time.Time
}

// NewSessionInput carries session and refresh-token creation.
type NewSessionInput struct {
	SessionID        string
	RefreshTokenID   string
	RefreshTokenHash string
	UserID           string
	DeviceID         string
	DeviceName       string
	IP               string
	UserAgent        string
	ActiveKey        string
	Now              time.Time
	SessionExpiresAt time.Time
}

// Repository is the auth feature's persistence boundary.
type Repository interface {
	CreateUserWithPassword(ctx context.Context, in CreateUserInput) (domain.User, error)
	FindUserForLogin(ctx context.Context, email string) (domain.LoginUser, bool, error)
	CreateSession(ctx context.Context, in NewSessionInput) error
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(encoded, password string) (bool, error)
}

// AccessTokens signs first-party access tokens.
type AccessTokens interface {
	SignAccessToken(user domain.User, sessionID string) (string, error)
}

// LoginLimiter guards password logins against brute force.
type LoginLimiter interface {
	AssertAllowed(ctx context.Context, email, ip string) error
	RecordFailure(ctx context.Context, email, ip string) error
	RecordSuccess(ctx context.Context, email, ip string) error
}
