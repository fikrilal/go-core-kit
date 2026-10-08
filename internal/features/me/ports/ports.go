// Package ports defines the me feature's dependency interfaces.
package ports

import (
	"context"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
)

// UpdateProfileInput carries a profile patch for one user.
type UpdateProfileInput struct {
	UserID  string
	Profile domain.ProfilePatch
	Now     time.Time
}

// Repository is the me feature's persistence boundary.
type Repository interface {
	GetMe(ctx context.Context, userID string) (domain.MeView, bool, error)
	UpdateProfile(ctx context.Context, in UpdateProfileInput) (domain.MeView, bool, error)
}
