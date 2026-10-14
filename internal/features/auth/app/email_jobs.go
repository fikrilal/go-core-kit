package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
)

// Email job outcomes.
const (
	EmailOutcomeSent            = "sent"
	EmailOutcomeUserNotFound    = "user_not_found"
	EmailOutcomeAlreadyDeleted  = "already_deleted"
	EmailOutcomeAlreadyVerified = "already_verified"
)

// EmailJobsConfig carries the email job policy knobs.
type EmailJobsConfig struct {
	VerificationTTL time.Duration
	ResetTTL        time.Duration
	PublicAppURL    string
}

// EmailJobDeps are the email job collaborators.
type EmailJobDeps struct {
	Repository ports.Repository
	Email      ports.EmailSender
	Clock      clock.Clock
}

// EmailJobOutcome reports one email job's ending and the provider id.
type EmailJobOutcome struct {
	Outcome string
	EmailID string
}

// EmailJobs implements the two auth email handlers.
type EmailJobs struct {
	repo   ports.Repository
	email  ports.EmailSender
	clock  clock.Clock
	config EmailJobsConfig
}

// NewEmailJobs builds the email job handlers.
func NewEmailJobs(config EmailJobsConfig, deps EmailJobDeps) *EmailJobs {
	return &EmailJobs{repo: deps.Repository, email: deps.Email, clock: deps.Clock, config: config}
}

// SendVerificationEmail mints a token and sends the verification email. A
// missing, deleted, or already verified account is a successful skip.
func (j *EmailJobs) SendVerificationEmail(ctx context.Context, userID string) (EmailJobOutcome, error) {
	state, found, err := j.repo.FindUserEmailState(ctx, userID)
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: find user: %w", err)
	}
	if !found {
		return EmailJobOutcome{Outcome: EmailOutcomeUserNotFound}, nil
	}
	if state.Status == domain.StatusDeleted {
		return EmailJobOutcome{Outcome: EmailOutcomeAlreadyDeleted}, nil
	}
	if !state.EmailVerifiedAt.IsZero() {
		return EmailJobOutcome{Outcome: EmailOutcomeAlreadyVerified}, nil
	}
	now := j.clock.Now()
	expiresAt := now.Add(j.config.VerificationTTL)
	token, err := domain.NewEmailVerificationToken()
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: create token: %w", err)
	}
	tokenID, err := uuid.NewV7()
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: generate token id: %w", err)
	}
	if err := j.repo.CreateEmailVerificationToken(ctx, ports.CreateAuthTokenInput{
		ID:        tokenID.String(),
		UserID:    state.ID,
		TokenHash: domain.HashEmailVerificationToken(token),
		ExpiresAt: expiresAt,
		Now:       now,
	}); err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: store token: %w", err)
	}
	message := email.VerificationEmail(
		state.Email,
		email.BrandName(j.config.PublicAppURL),
		token,
		email.VerifyEmailURL(j.config.PublicAppURL, token),
		expiresAt,
	)
	emailID, err := j.email.Send(ctx, message)
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: send verification email: %w", err)
	}
	return EmailJobOutcome{Outcome: EmailOutcomeSent, EmailID: emailID}, nil
}

// SendPasswordResetEmail mints a token and sends the reset email. The link is
// required for a reset, so an unset public URL fails the job and the retry
// policy applies; account skips still win.
func (j *EmailJobs) SendPasswordResetEmail(ctx context.Context, userID string) (EmailJobOutcome, error) {
	state, found, err := j.repo.FindUserEmailState(ctx, userID)
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: find user: %w", err)
	}
	if !found {
		return EmailJobOutcome{Outcome: EmailOutcomeUserNotFound}, nil
	}
	if state.Status == domain.StatusDeleted {
		return EmailJobOutcome{Outcome: EmailOutcomeAlreadyDeleted}, nil
	}
	if strings.TrimSpace(j.config.PublicAppURL) == "" {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: PUBLIC_APP_URL is not configured")
	}
	now := j.clock.Now()
	expiresAt := now.Add(j.config.ResetTTL)
	token, err := domain.NewPasswordResetToken()
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: create token: %w", err)
	}
	tokenID, err := uuid.NewV7()
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: generate token id: %w", err)
	}
	if err := j.repo.CreatePasswordResetToken(ctx, ports.CreateAuthTokenInput{
		ID:        tokenID.String(),
		UserID:    state.ID,
		TokenHash: domain.HashPasswordResetToken(token),
		ExpiresAt: expiresAt,
		Now:       now,
	}); err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: store token: %w", err)
	}
	message := email.PasswordResetEmail(
		state.Email,
		email.BrandName(j.config.PublicAppURL),
		token,
		email.ResetPasswordURL(j.config.PublicAppURL, token),
		expiresAt,
	)
	emailID, err := j.email.Send(ctx, message)
	if err != nil {
		return EmailJobOutcome{}, fmt.Errorf("auth email job: send reset email: %w", err)
	}
	return EmailJobOutcome{Outcome: EmailOutcomeSent, EmailID: emailID}, nil
}
