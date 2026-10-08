// Package app implements the me feature's use cases.
package app

import (
	"context"
	"fmt"
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
