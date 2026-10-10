package app

import (
	"context"
	"fmt"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
)

// accountDeletionGrace is the reference's 30-day grace period.
const accountDeletionGrace = 30 * 24 * time.Hour

// RequestAccountDeletionInput carries a deletion request for the caller.
type RequestAccountDeletionInput struct {
	UserID    string
	SessionID string
	TraceID   string
}

// RequestAccountDeletion schedules the deletion after the grace period.
// A repeated request keeps the stored schedule and reports success.
func (s *Service) RequestAccountDeletion(ctx context.Context, in RequestAccountDeletionInput) error {
	now := s.clock.Now()
	kind, err := s.repo.RequestAccountDeletion(ctx, ports.RequestAccountDeletionInput{
		UserID:       in.UserID,
		SessionID:    in.SessionID,
		TraceID:      in.TraceID,
		Now:          now,
		ScheduledFor: now.Add(accountDeletionGrace),
	})
	if err != nil {
		return fmt.Errorf("me: request account deletion: %w", err)
	}
	switch kind {
	case domain.DeletionOK, domain.DeletionAlreadyRequested, domain.DeletionNotRequested:
		return nil
	case domain.DeletionNotFound:
		return unauthorizedError()
	case domain.DeletionLastAdmin:
		return cannotDeleteLastAdminError()
	}
	return fmt.Errorf("me: unexpected deletion result %q", kind)
}

// CancelAccountDeletionInput carries a deletion cancel for the caller.
type CancelAccountDeletionInput struct {
	UserID    string
	SessionID string
	TraceID   string
}

// CancelAccountDeletion clears a scheduled deletion; cancelling twice is a
// no-op 204.
func (s *Service) CancelAccountDeletion(ctx context.Context, in CancelAccountDeletionInput) error {
	kind, err := s.repo.CancelAccountDeletion(ctx, ports.CancelAccountDeletionInput{
		UserID:    in.UserID,
		SessionID: in.SessionID,
		TraceID:   in.TraceID,
		Now:       s.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("me: cancel account deletion: %w", err)
	}
	switch kind {
	case domain.DeletionOK, domain.DeletionNotRequested, domain.DeletionAlreadyRequested:
		return nil
	case domain.DeletionNotFound:
		return unauthorizedError()
	case domain.DeletionLastAdmin:
		return cannotDeleteLastAdminError()
	}
	return fmt.Errorf("me: unexpected deletion result %q", kind)
}
