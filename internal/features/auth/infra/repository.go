//go:build !pg

// Package infra implements the auth feature's adapters.
package infra

import (
	"context"
	"errors"
	"fmt"

	"github.com/fikrilal/go-core-kit/internal/db/gen"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
)

// Repository is the sqlc-backed auth repository.
type Repository struct {
	database *db.Database
	queries  *gen.Queries
}

// NewRepository builds the repository.
func NewRepository(database *db.Database) *Repository {
	return &Repository{database: database, queries: gen.New(database.SQL())}
}

// CreateUserWithPassword creates the user and credential atomically.
func (r *Repository) CreateUserWithPassword(ctx context.Context, in ports.CreateUserInput) (domain.User, error) {
	now := utctime.Time{Time: in.Now}
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		if _, err := queries.CreateUser(ctx, gen.CreateUserParams{
			ID:        in.ID,
			Email:     in.Email,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			if db.IsUniqueViolation(err) {
				return domain.ErrEmailExists
			}
			return fmt.Errorf("create user: %w", err)
		}
		if err := queries.CreatePasswordCredential(ctx, gen.CreatePasswordCredentialParams{
			UserID:       in.ID,
			PasswordHash: in.PasswordHash,
			CreatedAt:    now,
			UpdatedAt:    now,
		}); err != nil {
			return fmt.Errorf("create password credential: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("create user with password: %w", err)
	}
	return domain.User{
		ID:            in.ID,
		Email:         in.Email,
		EmailVerified: false,
		Role:          domain.RoleUser,
		Status:        domain.StatusActive,
	}, nil
}

// FindUserForLogin loads the login core plus the stored password hash when set.
func (r *Repository) FindUserForLogin(ctx context.Context, email string) (domain.LoginUser, bool, error) {
	row, err := r.queries.GetUserForLogin(ctx, email)
	if errors.Is(err, db.ErrNoRows) {
		return domain.LoginUser{}, false, nil
	}
	if err != nil {
		return domain.LoginUser{}, false, fmt.Errorf("find user for login: %w", err)
	}
	user := domain.User{
		ID:            row.ID,
		Email:         row.Email,
		EmailVerified: !row.EmailVerifiedAt.IsZero(),
		Role:          row.Role,
		Status:        row.Status,
	}
	var passwordHash *string
	if row.PasswordHash.Valid {
		hash := row.PasswordHash.String
		passwordHash = &hash
	}
	return domain.LoginUser{User: user, PasswordHash: passwordHash}, true, nil
}

// CreateSession revokes any active session for the device and stores the new
// session with its refresh token in one transaction.
func (r *Repository) CreateSession(ctx context.Context, in ports.NewSessionInput) error {
	now := utctime.Time{Time: in.Now}
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		if in.ActiveKey != "" {
			revokeParams := gen.RevokeActiveSessionTokensParams{
				RevokedAt: now,
				ActiveKey: in.ActiveKey,
				UserID:    in.UserID,
			}
			if _, err := queries.RevokeActiveSessionTokens(ctx, revokeParams); err != nil {
				return fmt.Errorf("revoke active session tokens: %w", err)
			}
			if _, err := queries.RevokeSessionByActiveKey(ctx, gen.RevokeSessionByActiveKeyParams(revokeParams)); err != nil {
				return fmt.Errorf("revoke active session: %w", err)
			}
		}
		if err := queries.CreateSession(ctx, gen.CreateSessionParams{
			ID:         in.SessionID,
			UserID:     in.UserID,
			DeviceID:   in.DeviceID,
			DeviceName: in.DeviceName,
			Ip:         in.IP,
			UserAgent:  in.UserAgent,
			ActiveKey:  in.ActiveKey,
			LastSeenAt: now,
			ExpiresAt:  utctime.Time{Time: in.SessionExpiresAt},
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		if err := queries.CreateRefreshToken(ctx, gen.CreateRefreshTokenParams{
			ID:        in.RefreshTokenID,
			TokenHash: in.RefreshTokenHash,
			ExpiresAt: utctime.Time{Time: in.SessionExpiresAt},
			SessionID: in.SessionID,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("create refresh token: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}
