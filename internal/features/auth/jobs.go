//go:build !pg

package auth

import (
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/infra"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/email"
)

// Email job names and their shared payload.
const (
	EmailVerificationJob  = "auth.sendVerificationEmail"
	EmailPasswordResetJob = "auth.sendPasswordResetEmail" //nolint:gosec // job name, not a credential
)

// EmailJobPayload is the payload both auth email jobs carry.
type EmailJobPayload struct {
	UserID      string `json:"userId"`
	RequestedAt string `json:"requestedAt"`
}

// EmailJobsConfig carries the email job policy knobs.
type EmailJobsConfig struct {
	VerificationTTL time.Duration
	ResetTTL        time.Duration
	PublicAppURL    string
}

// EmailJobDeps are the email job dependencies.
type EmailJobDeps struct {
	Database *db.Database
	Email    email.Sender
	Clock    clock.Clock
}

// NewEmailJobs builds the auth email handlers for the worker.
func NewEmailJobs(config EmailJobsConfig, deps EmailJobDeps) *app.EmailJobs {
	return app.NewEmailJobs(app.EmailJobsConfig{
		VerificationTTL: config.VerificationTTL,
		ResetTTL:        config.ResetTTL,
		PublicAppURL:    config.PublicAppURL,
	}, app.EmailJobDeps{
		Repository: infra.NewRepository(deps.Database),
		Email:      deps.Email,
		Clock:      deps.Clock,
	})
}
