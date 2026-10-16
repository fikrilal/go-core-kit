// Package ports defines the auth feature's dependency interfaces.
package ports

import (
	"context"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
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

// RotateRefreshTokenInput carries a refresh rotation.
type RotateRefreshTokenInput struct {
	CurrentTokenHash string
	NextTokenID      string
	NextTokenHash    string
	Now              time.Time
	IP               string
	UserAgent        string
}

// ChangePasswordInput carries a password change with its compare-and-set guard.
type ChangePasswordInput struct {
	UserID                      string
	SessionID                   string
	ExpectedCurrentPasswordHash string
	NewPasswordHash             string
	Now                         time.Time
}

// CreateOidcUserInput carries an OIDC sign-up.
type CreateOidcUserInput struct {
	ID       string
	Email    string
	Profile  domain.Profile
	Provider string
	Subject  string
	Now      time.Time
}

// EmailVerificationTokenState is the token row the verify use case reads.
type EmailVerificationTokenState struct {
	ID         string
	UserID     string
	ExpiresAt  time.Time
	UsedAt     time.Time
	UserStatus string
}

// CreateAuthTokenInput carries a minted email token.
type CreateAuthTokenInput struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	Now       time.Time
}

// LinkExternalIdentityInput carries an OIDC identity link for the current user.
type LinkExternalIdentityInput struct {
	UserID   string
	Provider string
	Subject  string
	Email    string
	Now      time.Time
}

// JobScheduler enqueues jobs on a platform queue from the API side.
type JobScheduler interface {
	EnqueueAt(ctx context.Context, queue, job string, payload any, meta queue.Meta, at time.Time) error
}

// Repository is the auth feature's persistence boundary.
type Repository interface {
	CreateUserWithPassword(ctx context.Context, in CreateUserInput) (domain.User, error)
	FindUserForLogin(ctx context.Context, email string) (domain.LoginUser, bool, error)
	CreateSession(ctx context.Context, in NewSessionInput) error
	FindRefreshToken(ctx context.Context, tokenHash string) (domain.RefreshCredentials, bool, error)
	RotateRefreshToken(ctx context.Context, in RotateRefreshTokenInput) (domain.RotationKind, error)
	RevokeSessionByRefreshToken(ctx context.Context, tokenHash string, now time.Time) (bool, error)
	FindPasswordCredential(ctx context.Context, userID string) (string, bool, error)
	ChangePasswordAndRevokeOtherSessions(ctx context.Context, in ChangePasswordInput) (domain.ChangePasswordKind, error)
	FindUserIDByEmail(ctx context.Context, email string) (string, bool, error)
	FindUserStatus(ctx context.Context, userID string) (string, bool, error)
	FindUserByExternalIdentity(ctx context.Context, provider, subject string) (domain.User, bool, error)
	CreateUserWithExternalIdentity(ctx context.Context, in CreateOidcUserInput) (domain.User, error)
	LinkExternalIdentityToUser(ctx context.Context, in LinkExternalIdentityInput) (domain.LinkIdentityKind, error)
	GetMeView(ctx context.Context, userID string) (domain.MeView, bool, error)
	FindUserEmailState(ctx context.Context, userID string) (domain.UserEmailState, bool, error)
	CreateEmailVerificationToken(ctx context.Context, in CreateAuthTokenInput) error
	CreatePasswordResetToken(ctx context.Context, in CreateAuthTokenInput) error
	EmailVerificationTokenByHash(ctx context.Context, tokenHash string) (EmailVerificationTokenState, bool, error)
	ConsumeEmailVerificationTokenAndVerifyEmail(ctx context.Context, tokenID, userID string, now time.Time) (bool, error)
}

// EmailSender sends one transactional email and reports the provider id.
type EmailSender interface {
	Send(ctx context.Context, message email.Message) (string, error)
}

// OidcVerifier verifies OIDC id_tokens for a provider.
type OidcVerifier interface {
	VerifyIDToken(ctx context.Context, provider, idToken string) (domain.OidcVerification, error)
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
