package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

func newEmailJobsFixture(t *testing.T, publicAppURL string) (*app.EmailJobs, *fakeRepository, *fakeEmailSender) {
	t.Helper()
	f := newFixture(t)
	sender := &fakeEmailSender{}
	jobs := app.NewEmailJobs(
		app.EmailJobsConfig{
			VerificationTTL: 24 * time.Hour,
			ResetTTL:        30 * time.Minute,
			PublicAppURL:    publicAppURL,
		},
		app.EmailJobDeps{Repository: f.repo, Email: sender, Clock: clock.Fixed{Moment: moment}},
	)
	return jobs, f.repo, sender
}

func TestSendVerificationEmail(t *testing.T) {
	jobs, repo, sender := newEmailJobsFixture(t, "https://app.example.com")
	repo.emailState = domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusActive}
	repo.emailStateFound = true

	outcome, err := jobs.SendVerificationEmail(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("SendVerificationEmail() error = %v", err)
	}
	if outcome.Outcome != app.EmailOutcomeSent {
		t.Fatalf("outcome = %q, want sent", outcome.Outcome)
	}
	if len(repo.createdTokens) != 1 {
		t.Fatalf("tokens = %d, want 1", len(repo.createdTokens))
	}
	token := repo.createdTokens[0]
	if token.UserID != "user-1" || token.TokenHash == "" {
		t.Errorf("token = %+v, want the user and a hash", token)
	}
	if want := moment.Add(24 * time.Hour); !token.ExpiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, want the 24 h verification TTL %v", token.ExpiresAt, want)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("emails = %d, want 1", len(sender.sent))
	}
	message := sender.sent[0]
	if message.Subject != "Verify your email" || message.To != "user@example.com" {
		t.Errorf("message = %+v, want the verification email", message)
	}
	if !strings.Contains(message.Text, "https://app.example.com/verify-email?token=") {
		t.Errorf("text = %q, want the verification link", message.Text)
	}
}

func TestSendVerificationEmailSkips(t *testing.T) {
	cases := []struct {
		name  string
		state domain.UserEmailState
		found bool
		want  string
	}{
		{name: "missing user", want: app.EmailOutcomeUserNotFound},
		{name: "deleted user", state: domain.UserEmailState{ID: "user-1", Status: domain.StatusDeleted}, found: true, want: app.EmailOutcomeAlreadyDeleted},
		{name: "already verified", state: domain.UserEmailState{ID: "user-1", Status: domain.StatusActive, EmailVerifiedAt: moment}, found: true, want: app.EmailOutcomeAlreadyVerified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs, repo, sender := newEmailJobsFixture(t, "https://app.example.com")
			repo.emailState = tc.state
			repo.emailStateFound = tc.found
			outcome, err := jobs.SendVerificationEmail(context.Background(), "user-1")
			if err != nil {
				t.Fatalf("SendVerificationEmail() error = %v", err)
			}
			if outcome.Outcome != tc.want {
				t.Fatalf("outcome = %q, want %q", outcome, tc.want)
			}
			if len(repo.createdTokens) != 0 || len(sender.sent) != 0 {
				t.Error("skip stored a token or sent an email")
			}
		})
	}
}

func TestSendPasswordResetEmail(t *testing.T) {
	jobs, repo, sender := newEmailJobsFixture(t, "https://app.example.com")
	repo.emailState = domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusActive}
	repo.emailStateFound = true

	reset, err := jobs.SendPasswordResetEmail(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("SendPasswordResetEmail() error = %v", err)
	}
	if reset.Outcome != app.EmailOutcomeSent {
		t.Fatalf("outcome = %q, want sent", reset.Outcome)
	}
	if len(repo.createdTokens) != 1 {
		t.Fatalf("tokens = %d, want 1", len(repo.createdTokens))
	}
	if want := moment.Add(30 * time.Minute); !repo.createdTokens[0].ExpiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, want the 30 min reset TTL %v", repo.createdTokens[0].ExpiresAt, want)
	}
	if len(sender.sent) != 1 || sender.sent[0].Subject != "Reset your password" {
		t.Fatalf("emails = %+v, want the reset email", sender.sent)
	}
	if !strings.Contains(sender.sent[0].Text, "https://app.example.com/reset-password?token=") {
		t.Errorf("text = %q, want the reset link", sender.sent[0].Text)
	}
}

func TestSendPasswordResetEmailRequiresPublicURL(t *testing.T) {
	jobs, repo, sender := newEmailJobsFixture(t, "")
	repo.emailState = domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusActive}
	repo.emailStateFound = true

	if _, err := jobs.SendPasswordResetEmail(context.Background(), "user-1"); err == nil {
		t.Fatal("SendPasswordResetEmail() without PUBLIC_APP_URL = nil, want an error so the job retries")
	}
	if len(repo.createdTokens) != 0 || len(sender.sent) != 0 {
		t.Error("failed reset job stored a token or sent an email")
	}

	repo.emailStateFound = false
	reset, err := jobs.SendPasswordResetEmail(context.Background(), "missing")
	if err != nil || reset.Outcome != app.EmailOutcomeUserNotFound {
		t.Fatalf("skip with no URL = %q/%v, want the account skip to win", reset.Outcome, err)
	}
}

func TestSendVerificationEmailSurfacesFailures(t *testing.T) {
	jobs, repo, sender := newEmailJobsFixture(t, "https://app.example.com")
	repo.emailState = domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusActive}
	repo.emailStateFound = true
	sender.err = fmt.Errorf("provider down")

	if _, err := jobs.SendVerificationEmail(context.Background(), "user-1"); err == nil {
		t.Fatal("SendVerificationEmail() with a failing provider = nil, want an error")
	}
}
