//go:build !pg

// Package infra implements the me feature's adapters.
// This build is SQLite until a pg repository exists.
package infra

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/fikrilal/go-core-kit/internal/db/gen"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
)

// Repository is the sqlc-backed me repository.
type Repository struct {
	database *db.Database
	queries  *gen.Queries
}

// NewRepository builds the repository.
func NewRepository(database *db.Database) *Repository {
	return &Repository{database: database, queries: gen.New(database.SQL())}
}

// GetMe composes the user, profile, and auth methods.
func (r *Repository) GetMe(ctx context.Context, userID string) (domain.MeView, bool, error) {
	return meView(ctx, r.queries, userID)
}

// UpdateProfile touches the user, applies the patch to the stored profile, and
// re-reads the view, all in one transaction.
func (r *Repository) UpdateProfile(ctx context.Context, in ports.UpdateProfileInput) (domain.MeView, bool, error) {
	now := utctime.Time{Time: in.Now}
	var view domain.MeView
	found := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		touched, err := queries.TouchUser(ctx, gen.TouchUserParams{UpdatedAt: now, ID: in.UserID})
		if err != nil {
			return fmt.Errorf("touch user: %w", err)
		}
		if touched != 1 {
			return nil
		}
		if err := mergeProfilePatch(ctx, queries, in.UserID, in.Profile, now); err != nil {
			return err
		}
		view, found, err = meView(ctx, queries, in.UserID)
		return err
	})
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("update profile: %w", err)
	}
	return view, found, nil
}

// mergeProfilePatch applies the patch over the stored profile and upserts it.
func mergeProfilePatch(ctx context.Context, queries *gen.Queries, userID string, patch domain.ProfilePatch, now utctime.Time) error {
	existing, err := queries.GetUserProfile(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		existing = gen.GetUserProfileRow{}
	} else if err != nil {
		return fmt.Errorf("find user profile: %w", err)
	}
	if err := queries.UpsertUserProfile(ctx, gen.UpsertUserProfileParams{
		UserID:      userID,
		DisplayName: mergeField(patch.DisplayName, existing.DisplayName.Valid, existing.DisplayName.String),
		GivenName:   mergeField(patch.GivenName, existing.GivenName.Valid, existing.GivenName.String),
		FamilyName:  mergeField(patch.FamilyName, existing.FamilyName.Valid, existing.FamilyName.String),
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		return fmt.Errorf("upsert user profile: %w", err)
	}
	return nil
}

// mergeField applies one patch field: absent keeps the stored value, and an
// explicit null clears it. The SQL NULLIF turns the cleared empty string into
// NULL.
func mergeField(patch domain.Field, storedValid bool, storedValue string) string {
	if !patch.Present {
		if !storedValid {
			return ""
		}
		return storedValue
	}
	if patch.Null {
		return ""
	}
	return patch.Value
}

// meView composes the view; a deleted user reads as not found.
func meView(ctx context.Context, queries *gen.Queries, userID string) (domain.MeView, bool, error) {
	row, err := queries.GetMeView(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return domain.MeView{}, false, nil
	}
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("get me view: %w", err)
	}
	if row.UserStatus == domain.StatusDeleted {
		return domain.MeView{}, false, nil
	}
	methods, err := queries.GetAuthMethods(ctx, userID)
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("get auth methods: %w", err)
	}
	return domain.MeView{
		User: domain.User{
			ID:            row.UserID,
			Email:         row.UserEmail,
			EmailVerified: !row.UserEmailVerifiedAt.IsZero(),
			Role:          row.UserRole,
			Status:        row.UserStatus,
		},
		Profile: domain.Profile{
			DisplayName: row.ProfileDisplayName.String,
			GivenName:   row.ProfileGivenName.String,
			FamilyName:  row.ProfileFamilyName.String,
		},
		AuthMethods: orderAuthMethods(methods),
	}, true, nil
}

// orderAuthMethods sorts PASSWORD then GOOGLE. UNION does not order rows.
func orderAuthMethods(methods []string) []string {
	precedence := map[string]int{domain.AuthMethodPassword: 0, domain.AuthMethodGoogle: 1}
	ordered := make([]string, len(methods))
	copy(ordered, methods)
	slices.SortStableFunc(ordered, func(a, b string) int {
		return precedence[a] - precedence[b]
	})
	return ordered
}
