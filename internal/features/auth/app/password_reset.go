package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
)

// ConfirmPasswordReset consumes a reset token and replaces the credential. The
// token, the credential write, and the sign-out share one transaction, so a
// failure cannot spend a token without resetting the password.
func (s *Service) ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error {
	if err := s.checkPasswordPolicy(newPassword, "password"); err != nil {
		return err
	}
	// An empty token is the DTO's `required` validation, not this layer's job.
	token := strings.TrimSpace(rawToken)
	now := s.clock.Now()
	state, found, err := s.repo.TokenByHash(ctx, ports.TokenPasswordReset, domain.HashPasswordResetToken(token))
	if err != nil {
		return fmt.Errorf("auth: confirm password reset: %w", err)
	}
	if !found || state.UserStatus == domain.StatusDeleted {
		return passwordResetTokenInvalidError()
	}
	if !state.UsedAt.IsZero() || !state.ExpiresAt.After(now) {
		return passwordResetTokenExpiredError()
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("auth: confirm password reset: %w", err)
	}
	consumed, err := s.repo.ConsumePasswordResetTokenAndSetPassword(ctx, ports.PasswordResetConsumption{
		TokenID:      state.ID,
		UserID:       state.UserID,
		PasswordHash: hash,
		Now:          now,
	})
	if err != nil {
		return fmt.Errorf("auth: confirm password reset: %w", err)
	}
	if !consumed {
		// A concurrent request consumed the token first.
		return passwordResetTokenExpiredError()
	}
	return nil
}
