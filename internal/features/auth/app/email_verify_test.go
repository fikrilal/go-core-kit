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
)

func verificationFixture(t *testing.T) (*app.Service, *fakeRepository) {
	t.Helper()
	repo := &fakeRepository{
		verificationConsumed:   true,
		verificationTokenFound: true,
		verificationToken: ports.EmailVerificationTokenState{
			ID: "token-1", UserID: "user-1", ExpiresAt: moment.Add(time.Hour), UserStatus: domain.StatusActive,
		},
	}
	return app.New(app.Config{}, app.Deps{Repository: repo, Clock: clock.Fixed{Moment: moment}}), repo
}

func TestVerifyEmailConsumesTheToken(t *testing.T) {
	service, repo := verificationFixture(t)
	if err := service.VerifyEmail(context.Background(), "raw-token"); err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
	if len(repo.verifiedUsers) != 1 || repo.verifiedUsers[0] != "user-1" {
		t.Fatalf("verified = %v, want the token's user", repo.verifiedUsers)
	}
}

func TestVerifyEmailProblemCodes(t *testing.T) {
	tests := []struct {
		name     string
		found    bool
		consumed bool
		state    ports.EmailVerificationTokenState
		wantCode httpx.Code
	}{
		{name: "unknown token", wantCode: httpx.CodeAuthEmailVerificationTokenInvalid},
		{
			name: "deleted account", found: true, wantCode: httpx.CodeAuthEmailVerificationTokenInvalid,
			state: ports.EmailVerificationTokenState{UserStatus: domain.StatusDeleted, ExpiresAt: moment.Add(time.Hour)},
		},
		{name: "already used", found: true, state: ports.EmailVerificationTokenState{
			UserStatus: domain.StatusActive,
			UsedAt:     moment.Add(-time.Minute), ExpiresAt: moment.Add(time.Hour),
		}, wantCode: httpx.CodeAuthEmailVerificationTokenExpired},
		{name: "expired", found: true, state: ports.EmailVerificationTokenState{
			UserStatus: domain.StatusActive,
			ExpiresAt:  moment.Add(-time.Second),
		}, wantCode: httpx.CodeAuthEmailVerificationTokenExpired},
		{name: "lost the race", found: true, consumed: false, state: ports.EmailVerificationTokenState{
			UserStatus: domain.StatusActive,
			ExpiresAt:  moment.Add(time.Hour),
		}, wantCode: httpx.CodeAuthEmailVerificationTokenExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo := verificationFixture(t)
			repo.verificationTokenFound, repo.verificationConsumed = tt.found, tt.consumed
			if tt.found {
				repo.verificationToken = tt.state
			}
			err := service.VerifyEmail(context.Background(), "raw-token")
			var problem *httpx.ProblemError
			if !errors.As(err, &problem) {
				t.Fatalf("VerifyEmail() error = %v, want a problem", err)
			}
			if problem.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", problem.Code, tt.wantCode)
			}
			if len(repo.verifiedUsers) != 0 {
				t.Errorf("verified = %v, want no verification on a problem", repo.verifiedUsers)
			}
		})
	}
}
