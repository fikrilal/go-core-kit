// Package app implements the me feature's use cases.
package app

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

// maxProfileFieldLength mirrors the reference DTO bound.
const maxProfileFieldLength = 100

// Deps are the service's collaborators.
type Deps struct {
	Repository ports.Repository
	Clock      clock.Clock
}

// Service implements the me use cases.
type Service struct {
	repo  ports.Repository
	clock clock.Clock
}

// New builds the me service.
func New(deps Deps) *Service {
	return &Service{repo: deps.Repository, clock: deps.Clock}
}

// GetMe loads the authenticated user's Me view.
func (s *Service) GetMe(ctx context.Context, userID string) (domain.MeView, error) {
	view, found, err := s.repo.GetMe(ctx, userID)
	if err != nil {
		return domain.MeView{}, fmt.Errorf("me: get: %w", err)
	}
	if !found {
		return domain.MeView{}, unauthorizedError()
	}
	return view, nil
}

// PatchMeInput carries a profile patch for the authenticated user.
type PatchMeInput struct {
	UserID  string
	Profile domain.ProfilePatch
}

// PatchMe applies a profile patch and returns the fresh Me view.
func (s *Service) PatchMe(ctx context.Context, in PatchMeInput) (domain.MeView, error) {
	if err := validateProfilePatch(in.Profile); err != nil {
		return domain.MeView{}, err
	}
	view, found, err := s.repo.UpdateProfile(ctx, ports.UpdateProfileInput{
		UserID:  in.UserID,
		Profile: in.Profile,
		Now:     s.clock.Now(),
	})
	if err != nil {
		return domain.MeView{}, fmt.Errorf("me: patch: %w", err)
	}
	if !found {
		return domain.MeView{}, unauthorizedError()
	}
	return view, nil
}

// ListSessionsInput carries a parsed list request.
type ListSessionsInput struct {
	UserID    string
	SessionID string
	Query     httpx.ListQuery
}

// SessionPage is one page of session views with its cursor metadata.
type SessionPage struct {
	Views      []domain.SessionView
	HasMore    bool
	NextCursor string
}

// ListSessions loads the user's sessions and computes their status.
func (s *Service) ListSessions(ctx context.Context, in ListSessionsInput) (SessionPage, error) {
	if err := s.requireUser(ctx, in.UserID); err != nil {
		return SessionPage{}, err
	}
	page, err := s.repo.ListSessions(ctx, ports.ListSessionsInput{
		UserID: in.UserID,
		Limit:  in.Query.Limit,
		Sort:   sortFields(in.Query.Sort),
		Cursor: in.Query.Cursor,
	})
	if err != nil {
		return SessionPage{}, fmt.Errorf("me: list sessions: %w", err)
	}
	now := s.clock.Now()
	views := make([]domain.SessionView, 0, len(page.Sessions))
	for _, session := range page.Sessions {
		views = append(views, sessionView(session, in.SessionID, now))
	}
	return SessionPage{Views: views, HasMore: page.HasMore, NextCursor: page.NextCursor}, nil
}

// RevokeSessionInput carries a session revocation for the authenticated user.
type RevokeSessionInput struct {
	UserID    string
	SessionID string
}

// RevokeSession revokes one of the user's sessions; unknown ids are 404.
func (s *Service) RevokeSession(ctx context.Context, in RevokeSessionInput) error {
	if err := s.requireUser(ctx, in.UserID); err != nil {
		return err
	}
	revoked, err := s.repo.RevokeSession(ctx, in.UserID, in.SessionID, s.clock.Now())
	if err != nil {
		return fmt.Errorf("me: revoke session: %w", err)
	}
	if !revoked {
		return sessionNotFoundError()
	}
	return nil
}

// requireUser rejects missing or deleted accounts as unusable tokens.
func (s *Service) requireUser(ctx context.Context, userID string) error {
	status, found, err := s.repo.FindUserStatus(ctx, userID)
	if err != nil {
		return fmt.Errorf("me: find user: %w", err)
	}
	if !found || status == domain.StatusDeleted {
		return unauthorizedError()
	}
	return nil
}

// sessionView computes the status and the current-session flag.
func sessionView(session domain.Session, currentSessionID string, now time.Time) domain.SessionView {
	status := domain.SessionActive
	switch {
	case !session.RevokedAt.IsZero():
		status = domain.SessionRevoked
	case !session.ExpiresAt.After(now):
		status = domain.SessionExpired
	}
	return domain.SessionView{Session: session, Current: session.ID == currentSessionID, Status: status}
}

// sortFields converts the parsed wire sort into the port sort.
func sortFields(fields []httpx.SortField) []ports.SortField {
	sort := make([]ports.SortField, 0, len(fields))
	for _, field := range fields {
		sort = append(sort, ports.SortField{Field: field.Field, Desc: field.Desc})
	}
	return sort
}

// validateProfilePatch collects every issue in field order.
func validateProfilePatch(patch domain.ProfilePatch) error {
	if !patch.Defined() {
		return profilePatchRequiredError()
	}
	fields := []struct {
		name  string
		field domain.Field
	}{
		{"displayName", patch.DisplayName},
		{"givenName", patch.GivenName},
		{"familyName", patch.FamilyName},
	}
	var issues []httpx.FieldIssue
	for _, item := range fields {
		if !item.field.Present || item.field.Null {
			continue
		}
		switch {
		case item.field.Value == "":
			issues = append(issues, httpx.FieldIssue{Field: item.name, Message: "must not be empty"})
		case utf8.RuneCountInString(item.field.Value) > maxProfileFieldLength:
			issues = append(issues, httpx.FieldIssue{
				Field:   item.name,
				Message: fmt.Sprintf("must be at most %d characters", maxProfileFieldLength),
			})
		}
	}
	if len(issues) > 0 {
		return validationFailedError(issues)
	}
	return nil
}
