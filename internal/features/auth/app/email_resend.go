package app

import (
	"context"
	"fmt"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
)

// ResendVerificationEmail applies the resend gates in order: the provider gate,
// the per-account limit, then the account state. It reports whether the caller
// may be sent a fresh verification email: a missing or deleted account answers
// success without enqueuing, so the route cannot be used to probe which
// addresses exist. The route enqueues only when this reports true.
func (s *Service) ResendVerificationEmail(ctx context.Context, userID string) (bool, error) {
	if s.scheduler == nil || !s.emailEnabled {
		return false, verificationEmailNotConfiguredError()
	}
	// The limit applies before the account check: every attempt counts, so a
	// caller cannot hammer the route, and the answer stays the same whether or
	// not the account exists.
	if err := s.resendLimiter.AssertAllowed(ctx, userID, ""); err != nil {
		return false, fmt.Errorf("auth: resend verification email: %w", err)
	}
	if err := s.resendLimiter.RecordFailure(ctx, userID, ""); err != nil {
		return false, fmt.Errorf("auth: resend verification email: %w", err)
	}
	state, found, err := s.repo.FindUserEmailState(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("auth: resend verification email: %w", err)
	}
	if !found || state.Status == domain.StatusDeleted {
		return false, nil
	}
	return true, nil
}
