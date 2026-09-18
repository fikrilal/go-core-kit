//go:build !pg

// Package infra implements the auth feature's adapters.
package infra

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// FindRefreshToken loads a presented refresh token with its session and user.
func (r *Repository) FindRefreshToken(ctx context.Context, tokenHash string) (domain.RefreshCredentials, bool, error) {
	row, err := r.queries.GetRefreshTokenWithSession(ctx, tokenHash)
	if errors.Is(err, db.ErrNoRows) {
		return domain.RefreshCredentials{}, false, nil
	}
	if err != nil {
		return domain.RefreshCredentials{}, false, fmt.Errorf("find refresh token: %w", err)
	}
	var replacedByID *string
	if row.TokenReplacedByID.Valid {
		value := row.TokenReplacedByID.String
		replacedByID = &value
	}
	credentials := domain.RefreshCredentials{
		User: domain.User{
			ID:            row.UserID,
			Email:         row.UserEmail,
			EmailVerified: !row.UserEmailVerifiedAt.IsZero(),
			Role:          row.UserRole,
			Status:        row.UserStatus,
		},
		Session: domain.LoginSession{
			ID:        row.SessionID,
			ExpiresAt: row.SessionExpiresAt.Time,
			Revoked:   !row.SessionRevokedAt.IsZero(),
		},
		Token: domain.RefreshToken{
			ID:           row.TokenID,
			ExpiresAt:    row.TokenExpiresAt.Time,
			Revoked:      !row.TokenRevokedAt.IsZero(),
			ReplacedByID: replacedByID,
		},
	}
	return credentials, true, nil
}

// RotateRefreshToken rotates the presented token inside one transaction,
// containing reuse by revoking the whole session family.
func (r *Repository) RotateRefreshToken(ctx context.Context, in ports.RotateRefreshTokenInput) (domain.RotationKind, error) {
	kind := domain.RotationOK
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		existing, err := queries.GetRefreshTokenWithSession(ctx, in.CurrentTokenHash)
		if errors.Is(err, db.ErrNoRows) {
			kind = domain.RotationNotFound
			return nil
		}
		if err != nil {
			return fmt.Errorf("find refresh token: %w", err)
		}
		kind, err = rotateExisting(ctx, queries, existing, in)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("rotate refresh token: %w", err)
	}
	return kind, nil
}

func rotateExisting(ctx context.Context, queries *gen.Queries, existing gen.GetRefreshTokenWithSessionRow, in ports.RotateRefreshTokenInput) (domain.RotationKind, error) {
	now := utctime.Time{Time: in.Now}
	if !existing.SessionRevokedAt.IsZero() {
		return domain.RotationSessionRevoked, nil
	}
	if !existing.TokenExpiresAt.After(in.Now) || !existing.SessionExpiresAt.After(in.Now) {
		return domain.RotationExpired, nil
	}
	if !existing.TokenRevokedAt.IsZero() || existing.TokenReplacedByID.Valid {
		if err := revokeSessionFamily(ctx, queries, existing.SessionID, now); err != nil {
			return "", err
		}
		return domain.RotationReused, nil
	}
	if err := queries.CreateRefreshToken(ctx, gen.CreateRefreshTokenParams{
		ID:        in.NextTokenID,
		TokenHash: in.NextTokenHash,
		ExpiresAt: existing.TokenExpiresAt,
		SessionID: existing.SessionID,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		return "", fmt.Errorf("create next refresh token: %w", err)
	}
	updated, err := queries.MarkRefreshTokenRotated(ctx, gen.MarkRefreshTokenRotatedParams{
		RevokedAt:    now,
		ReplacedByID: in.NextTokenID,
		ID:           existing.TokenID,
	})
	if err != nil {
		return "", fmt.Errorf("mark refresh token rotated: %w", err)
	}
	if updated != 1 {
		if err := revokeSessionFamily(ctx, queries, existing.SessionID, now); err != nil {
			return "", err
		}
		return domain.RotationReused, nil
	}
	if err := queries.TouchSession(ctx, gen.TouchSessionParams{
		LastSeenAt: now,
		Ip:         in.IP,
		UserAgent:  in.UserAgent,
		UpdatedAt:  now,
		ID:         existing.SessionID,
	}); err != nil {
		return "", fmt.Errorf("touch session: %w", err)
	}
	return domain.RotationOK, nil
}

// RevokeSessionByRefreshToken revokes the session behind a token hash.
func (r *Repository) RevokeSessionByRefreshToken(ctx context.Context, tokenHash string, now time.Time) (bool, error) {
	revoked := false
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		sessionID, err := queries.GetSessionIDByRefreshTokenHash(ctx, tokenHash)
		if errors.Is(err, db.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("find session for refresh token: %w", err)
		}
		if err := revokeSessionFamily(ctx, queries, sessionID, utctime.Time{Time: now}); err != nil {
			return err
		}
		revoked = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("revoke session by refresh token: %w", err)
	}
	return revoked, nil
}

func revokeSessionFamily(ctx context.Context, queries *gen.Queries, sessionID string, now utctime.Time) error {
	if err := queries.ClearSessionDeviceState(ctx, gen.ClearSessionDeviceStateParams{Now: now, ID: sessionID}); err != nil {
		return fmt.Errorf("clear session device state: %w", err)
	}
	if _, err := queries.RevokeSession(ctx, gen.RevokeSessionParams{RevokedAt: now, ID: sessionID}); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if _, err := queries.RevokeSessionRefreshTokens(ctx, gen.RevokeSessionRefreshTokensParams{RevokedAt: now, SessionID: sessionID}); err != nil {
		return fmt.Errorf("revoke session refresh tokens: %w", err)
	}
	return nil
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
