package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

func TestRequestPasswordResetGatesAndNoOracle(t *testing.T) {
	tests := []struct {
		name         string
		scheduler    bool
		emailEnabled bool
		found        bool
		state        domain.UserEmailState
		deny         bool
		wantCode     httpx.Code
		wantUserID   string
		wantFailures int
	}{
		{name: "no scheduler answers not configured", wantCode: httpx.CodeAuthEmailNotConfigured},
		{name: "no sender answers not configured", scheduler: true, wantCode: httpx.CodeAuthEmailNotConfigured},
		{name: "limited caller is refused", scheduler: true, emailEnabled: true, deny: true, wantCode: httpx.CodeRateLimited},
		{name: "unknown address has no oracle", scheduler: true, emailEnabled: true, wantFailures: 1},
		{
			name: "deleted account has no oracle", scheduler: true, emailEnabled: true, found: true, wantFailures: 1,
			state: domain.UserEmailState{Status: domain.StatusDeleted},
		},
		{
			name: "active account is mailed", scheduler: true, emailEnabled: true, found: true, wantFailures: 1,
			state: domain.UserEmailState{ID: "user-1", Status: domain.StatusActive}, wantUserID: "user-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{emailState: tt.state, emailStateFound: tt.found}
			limiter := &stubLimiter{deny: tt.deny}
			scheduler := &stubScheduler{}
			deps := app.Deps{
				Repository: repo, Clock: clock.Fixed{Moment: moment},
				ResendLimiter: limiter, RequestLimiter: limiter, EmailEnabled: tt.emailEnabled,
			}
			if tt.scheduler {
				deps.Scheduler = scheduler
			}
			service := app.New(app.Config{}, deps)

			userID, err := service.RequestPasswordReset(context.Background(), "Reset@Example.com")
			assertRequestOutcome(t, err, userID, tt.wantCode, tt.wantUserID, limiter, scheduler, tt.wantFailures)
		})
	}
}

// assertRequestOutcome checks the problem, the account to mail, the attempts the
// limiter recorded, and that the service never enqueued anything itself.
func assertRequestOutcome(t *testing.T, err error, userID string, wantCode httpx.Code, wantUserID string, limiter *stubLimiter, scheduler *stubScheduler, wantFailures int) {
	t.Helper()
	if wantCode == "" {
		if err != nil {
			t.Fatalf("RequestPasswordReset() error = %v", err)
		}
	} else {
		var problem *httpx.ProblemError
		if !errors.As(err, &problem) || problem.Code != wantCode {
			t.Fatalf("error = %v, want the %s problem", err, wantCode)
		}
	}
	if userID != wantUserID {
		t.Errorf("userID = %q, want %q", userID, wantUserID)
	}
	if limiter.failures != wantFailures {
		t.Errorf("recorded attempts = %d, want %d", limiter.failures, wantFailures)
	}
	if scheduler.enqueued != 0 {
		t.Errorf("enqueued = %d, want the route to own enqueuing", scheduler.enqueued)
	}
}
