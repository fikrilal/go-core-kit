package app

import (
	"context"
	"fmt"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
)

// RequestPasswordReset applies the request gates in order: the provider gate, the
// per-address limit, then the account lookup. It reports the account a fresh
// reset email may be sent to, and an empty id for an unknown or deleted address,
// so the route answers the same way either way and cannot enumerate accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string) (string, error) {
	if s.scheduler == nil || !s.emailEnabled {
		return "", verificationEmailNotConfiguredError()
	}
	// The limit is keyed by the address the caller claims, so an unknown address
	// is metered too.
	email := domain.NormalizeEmail(rawEmail)
	if err := s.requestLimiter.AssertAllowed(ctx, email, ""); err != nil {
		return "", fmt.Errorf("auth: request password reset: %w", err)
	}
	if err := s.requestLimiter.RecordFailure(ctx, email, ""); err != nil {
		return "", fmt.Errorf("auth: request password reset: %w", err)
	}
	state, found, err := s.repo.UserEmailStateByEmail(ctx, email)
	if err != nil {
		return "", fmt.Errorf("auth: request password reset: %w", err)
	}
	if !found || state.Status == domain.StatusDeleted {
		return "", nil
	}
	return state.ID, nil
}
