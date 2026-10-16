package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
)

// VerifyEmail consumes a verification token and marks the account verified.
// Replaying a token, or presenting one past its expiry, answers with the expired
// code; an unknown token and an unverifiable account share the invalid code so
// the endpoint cannot be used to probe which accounts exist.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	// An empty token is the DTO's `required` validation, not this layer's job.
	token := strings.TrimSpace(rawToken)
	now := s.clock.Now()
	state, found, err := s.repo.EmailVerificationTokenByHash(ctx, domain.HashEmailVerificationToken(token))
	if err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}
	if !found || state.UserStatus == domain.StatusDeleted {
		return emailVerificationTokenInvalidError()
	}
	if !state.UsedAt.IsZero() || !state.ExpiresAt.After(now) {
		return emailVerificationTokenExpiredError()
	}
	// Consumption and verification share one transaction: a failure cannot
	// leave a token spent without the address verified.
	consumed, err := s.repo.ConsumeEmailVerificationTokenAndVerifyEmail(ctx, state.ID, state.UserID, now)
	if err != nil {
		return fmt.Errorf("auth: verify email: %w", err)
	}
	if !consumed {
		// A concurrent request consumed the token first.
		return emailVerificationTokenExpiredError()
	}
	return nil
}
