package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/password"
)

const resetPassword = "correct-horse-battery-staple"

func confirmService(t *testing.T) (*app.Service, *fakeRepository) {
	t.Helper()
	repo := &fakeRepository{
		resetConsumed:   true,
		resetTokenFound: true,
		resetToken: ports.TokenState{
			ID: "token-1", UserID: "user-1", ExpiresAt: moment.Add(30 * time.Minute), UserStatus: domain.StatusActive,
		},
	}
	return app.New(app.Config{PasswordMinLength: 10}, app.Deps{
		Repository: repo, Clock: clock.Fixed{Moment: moment}, Hasher: password.New(),
	}), repo
}

func TestConfirmPasswordResetReplacesTheCredential(t *testing.T) {
	service, repo := confirmService(t)
	if err := service.ConfirmPasswordReset(context.Background(), "raw-token", resetPassword); err != nil {
		t.Fatalf("ConfirmPasswordReset() error = %v", err)
	}
	if len(repo.resetPasswords) != 1 {
		t.Fatalf("consumptions = %v, want one", repo.resetPasswords)
	}
	hash := repo.resetPasswords[0].PasswordHash
	// The service must store a hash, never the password itself; the round trip
	// through the real hasher is the integration test's job.
	if hash == "" || hash == resetPassword {
		t.Fatalf("hash = %q, want a stored hash", hash)
	}
}

func TestConfirmPasswordResetProblemCodes(t *testing.T) {
	tests := []struct {
		name     string
		found    bool
		consumed bool
		state    ports.TokenState
		wantCode httpx.Code
	}{
		{name: "unknown token", wantCode: httpx.CodeAuthPasswordResetTokenInvalid},
		{
			name: "deleted account", found: true, wantCode: httpx.CodeAuthPasswordResetTokenInvalid,
			state: ports.TokenState{UserStatus: domain.StatusDeleted, ExpiresAt: moment.Add(time.Hour)},
		},
		{
			name: "used token", found: true, consumed: true, wantCode: httpx.CodeAuthPasswordResetTokenExpired,
			state: ports.TokenState{UserStatus: domain.StatusActive, UsedAt: moment.Add(-time.Minute), ExpiresAt: moment.Add(time.Hour)},
		},
		{
			name: "expired token", found: true, consumed: true, wantCode: httpx.CodeAuthPasswordResetTokenExpired,
			state: ports.TokenState{UserStatus: domain.StatusActive, ExpiresAt: moment.Add(-time.Second)},
		},
		{
			name: "lost the race", found: true, wantCode: httpx.CodeAuthPasswordResetTokenExpired,
			state: ports.TokenState{UserStatus: domain.StatusActive, ExpiresAt: moment.Add(time.Hour)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo := confirmService(t)
			repo.resetTokenFound, repo.resetConsumed = tt.found, tt.consumed
			if tt.found {
				repo.resetToken = tt.state
			}
			err := service.ConfirmPasswordReset(context.Background(), "raw-token", resetPassword)
			var problem *httpx.ProblemError
			if !errors.As(err, &problem) || problem.Code != tt.wantCode {
				t.Fatalf("error = %v, want the %s problem", err, tt.wantCode)
			}
			if len(repo.resetPasswords) != 0 {
				t.Errorf("consumptions = %v, want the credential untouched", repo.resetPasswords)
			}
		})
	}
}

func TestConfirmPasswordResetEnforcesTheLengthPolicy(t *testing.T) {
	service, repo := confirmService(t)
	if err := service.ConfirmPasswordReset(context.Background(), "raw-token", "short"); err == nil {
		t.Fatal("ConfirmPasswordReset(short) error = nil, want the validation problem")
	}
	if len(repo.resetPasswords) != 0 {
		t.Errorf("consumptions = %v, want the token untouched", repo.resetPasswords)
	}
}
