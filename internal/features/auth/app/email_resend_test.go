package app_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/queue"
)

// stubLimiter is the account-keyed limiter the resend route uses.
type stubLimiter struct {
	deny     bool
	failures int
}

func (l *stubLimiter) AssertAllowed(context.Context, string, string) error {
	if l.deny {
		return &httpx.ProblemError{Status: http.StatusTooManyRequests, Code: httpx.CodeRateLimited}
	}
	return nil
}

func (l *stubLimiter) RecordFailure(context.Context, string, string) error {
	l.failures++
	return nil
}

func (l *stubLimiter) RecordSuccess(context.Context, string, string) error { return nil }

// stubScheduler records what the service asks to enqueue; the service itself
// must never enqueue, only the route does.
type stubScheduler struct{ enqueued int }

func (s *stubScheduler) EnqueueAt(context.Context, string, string, any, queue.Meta, time.Time) error {
	s.enqueued++
	return nil
}

func TestResendVerificationEmailGatesAndSkips(t *testing.T) {
	tests := []struct {
		name         string
		scheduler    bool
		emailEnabled bool
		found        bool
		state        domain.UserEmailState
		deny         bool
		wantCode     httpx.Code
		wantFailures int
		wantEnqueue  bool
	}{
		{name: "no scheduler answers not configured", wantCode: httpx.CodeAuthEmailNotConfigured},
		{name: "no sender answers not configured", scheduler: true, wantCode: httpx.CodeAuthEmailNotConfigured},
		{name: "missing account skips silently", scheduler: true, emailEnabled: true, wantFailures: 1},
		{
			name: "deleted account skips silently", scheduler: true, emailEnabled: true, found: true,
			state: domain.UserEmailState{Status: domain.StatusDeleted}, wantFailures: 1,
		},
		{
			name: "suspended account may resend", scheduler: true, emailEnabled: true, found: true,
			state: domain.UserEmailState{Status: domain.StatusSuspended}, wantFailures: 1, wantEnqueue: true,
		},
		{
			name: "limited account is refused", scheduler: true, emailEnabled: true, found: true, deny: true,
			state: domain.UserEmailState{Status: domain.StatusActive}, wantCode: httpx.CodeRateLimited,
		},
		{
			name: "active account is allowed", scheduler: true, emailEnabled: true, found: true,
			state: domain.UserEmailState{Status: domain.StatusActive}, wantFailures: 1, wantEnqueue: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepository{emailState: tt.state, emailStateFound: tt.found}
			limiter := &stubLimiter{deny: tt.deny}
			scheduler := &stubScheduler{}
			deps := app.Deps{
				Repository: repo, Clock: clock.Fixed{Moment: moment},
				Limiter: limiter, ResendLimiter: limiter, EmailEnabled: tt.emailEnabled,
			}
			if tt.scheduler {
				deps.Scheduler = scheduler
			}
			service := app.New(app.Config{}, deps)

			shouldEnqueue, err := service.ResendVerificationEmail(context.Background(), "user-1")
			assertResendOutcome(t, err, tt.wantCode, limiter, scheduler, tt.wantFailures, shouldEnqueue, tt.wantEnqueue)
		})
	}
}

// assertResendOutcome checks the problem the service reported, the attempts the
// limiter recorded, and that the service never enqueued anything itself.
func assertResendOutcome(t *testing.T, err error, wantCode httpx.Code, limiter *stubLimiter, scheduler *stubScheduler, wantFailures int, shouldEnqueue, wantEnqueue bool) {
	t.Helper()
	if shouldEnqueue != wantEnqueue {
		t.Errorf("shouldEnqueue = %t, want %t", shouldEnqueue, wantEnqueue)
	}
	if wantCode == "" {
		if err != nil {
			t.Fatalf("ResendVerificationEmail() error = %v", err)
		}
	} else {
		var problem *httpx.ProblemError
		if !errors.As(err, &problem) || problem.Code != wantCode {
			t.Fatalf("error = %v, want the %s problem", err, wantCode)
		}
	}
	if limiter.failures != wantFailures {
		t.Errorf("recorded attempts = %d, want %d", limiter.failures, wantFailures)
	}
	if scheduler.enqueued != 0 {
		t.Errorf("enqueued = %d, want the route to own enqueuing", scheduler.enqueued)
	}
}
