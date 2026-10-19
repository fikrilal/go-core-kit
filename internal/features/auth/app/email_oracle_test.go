package app_test

import (
	"context"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/oracle"
)

// TestEmailJobsFollowTheOracle drives both email handlers from the shared matrix:
// the states and the outcomes they must produce are one table, so this test and the
// worker's assertions cannot disagree about them.
func TestEmailJobsFollowTheOracle(t *testing.T) {
	handlers := map[string]func(*app.EmailJobs, context.Context) (app.EmailJobOutcome, error){
		"verification": func(jobs *app.EmailJobs, ctx context.Context) (app.EmailJobOutcome, error) {
			return jobs.SendVerificationEmail(ctx, "user-1")
		},
		"password reset": func(jobs *app.EmailJobs, ctx context.Context) (app.EmailJobOutcome, error) {
			return jobs.SendPasswordResetEmail(ctx, "user-1")
		},
	}
	matrices := map[string]map[oracle.AccountState]oracle.EmailExpectation{
		"verification":   oracle.Verification,
		"password reset": oracle.PasswordReset,
	}

	for job, run := range handlers {
		for _, state := range oracle.States() {
			t.Run(job+"/"+string(state), func(t *testing.T) {
				jobs, repo, sender := newEmailJobsFixture(t, "https://app.example.com")
				repo.emailState, repo.emailStateFound = accountFor(state)
				want := matrices[job][state]

				outcome, err := run(jobs, context.Background())
				if err != nil {
					t.Fatalf("handler error = %v", err)
				}
				if outcome.Outcome != want.Outcome {
					t.Errorf("outcome = %q, want %q", outcome.Outcome, want.Outcome)
				}
				if sent := len(sender.sent) > 0; sent != want.Sends {
					t.Errorf("sent = %t, want %t for the %s state", sent, want.Sends, state)
				}
				if minted := len(repo.createdTokens); (minted > 0) != want.Sends {
					t.Errorf("tokens minted = %d, want sends=%t", minted, want.Sends)
				}
			})
		}
	}
}

// accountFor builds the repository state the oracle's row describes.
func accountFor(state oracle.AccountState) (domain.UserEmailState, bool) {
	switch state {
	case oracle.Missing:
		return domain.UserEmailState{}, false
	case oracle.Deleted:
		return domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusDeleted}, true
	case oracle.Suspended:
		return domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusSuspended}, true
	default:
		return domain.UserEmailState{ID: "user-1", Email: "user@example.com", Status: domain.StatusActive}, true
	}
}
