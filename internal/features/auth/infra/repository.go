//go:build !pg

// Package infra implements the auth feature's adapters.
// This build is SQLite until a pg repository exists.
package infra

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

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
// The returned user is constructed. The token uses it. The HTTP body uses MeView.
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

// FindPasswordCredential returns the stored password hash when one exists.
func (r *Repository) FindPasswordCredential(ctx context.Context, userID string) (string, bool, error) {
	hash, err := r.queries.GetPasswordCredentialHash(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find password credential: %w", err)
	}
	return hash, true, nil
}

// ChangePasswordAndRevokeOtherSessions updates the credential behind a
// compare-and-set guard and revokes every other active session.
func (r *Repository) ChangePasswordAndRevokeOtherSessions(ctx context.Context, in ports.ChangePasswordInput) (domain.ChangePasswordKind, error) {
	now := utctime.Time{Time: in.Now}
	kind := domain.ChangePasswordOK
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		queries := gen.New(tx)
		if _, err := queries.GetUserByID(ctx, in.UserID); err != nil {
			if errors.Is(err, db.ErrNoRows) {
				kind = domain.ChangePasswordNotFound
				return nil
			}
			return fmt.Errorf("find user: %w", err)
		}
		credential, err := queries.GetPasswordCredentialHash(ctx, in.UserID)
		if errors.Is(err, db.ErrNoRows) {
			kind = domain.ChangePasswordPasswordNotSet
			return nil
		}
		if err != nil {
			return fmt.Errorf("find password credential: %w", err)
		}
		if credential != in.ExpectedCurrentPasswordHash {
			kind = domain.ChangePasswordCurrentPasswordMismatch
			return nil
		}
		if err := queries.UpdatePasswordCredentialHash(ctx, gen.UpdatePasswordCredentialHashParams{
			PasswordHash: in.NewPasswordHash,
			UpdatedAt:    now,
			UserID:       in.UserID,
		}); err != nil {
			return fmt.Errorf("update password credential: %w", err)
		}
		if _, err := queries.RevokeOtherSessions(ctx, gen.RevokeOtherSessionsParams{
			RevokedAt: now,
			UserID:    in.UserID,
			SessionID: in.SessionID,
		}); err != nil {
			return fmt.Errorf("revoke other sessions: %w", err)
		}
		if _, err := queries.RevokeOtherSessionRefreshTokens(ctx, gen.RevokeOtherSessionRefreshTokensParams{
			RevokedAt: now,
			UserID:    in.UserID,
			SessionID: in.SessionID,
		}); err != nil {
			return fmt.Errorf("revoke other session tokens: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("change password: %w", err)
	}
	return kind, nil
}

// FindUserIDByEmail resolves a user id by email.
func (r *Repository) FindUserIDByEmail(ctx context.Context, email string) (string, bool, error) {
	id, err := r.queries.GetUserIDByEmail(ctx, email)
	if errors.Is(err, db.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find user id by email: %w", err)
	}
	return id, true, nil
}

// FindUserStatus returns the account status, reporting whether the user exists.
func (r *Repository) FindUserStatus(ctx context.Context, userID string) (string, bool, error) {
	status, err := r.queries.GetUserStatus(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find user status: %w", err)
	}
	return status, true, nil
}

// FindUserByExternalIdentity resolves the user behind a provider identity.
func (r *Repository) FindUserByExternalIdentity(ctx context.Context, provider, subject string) (domain.User, bool, error) {
	row, err := r.queries.GetUserByExternalIdentity(ctx, gen.GetUserByExternalIdentityParams{
		Provider: provider,
		Subject:  subject,
	})
	if errors.Is(err, db.ErrNoRows) {
		return domain.User{}, false, nil
	}
	if err != nil {
		return domain.User{}, false, fmt.Errorf("find user by external identity: %w", err)
	}
	return domain.User{
		ID:            row.ID,
		Email:         row.Email,
		EmailVerified: !row.EmailVerifiedAt.IsZero(),
		Role:          row.Role,
		Status:        row.Status,
	}, true, nil
}

// CreateUserWithExternalIdentity creates the verified user, profile, and identity atomically.
func (r *Repository) CreateUserWithExternalIdentity(ctx context.Context, in ports.CreateOidcUserInput) (domain.User, error) {
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
		if err := queries.SetUserEmailVerified(ctx, gen.SetUserEmailVerifiedParams{
			EmailVerifiedAt: now,
			UpdatedAt:       now,
			ID:              in.ID,
		}); err != nil {
			return fmt.Errorf("mark user verified: %w", err)
		}
		if err := queries.CreateUserProfile(ctx, gen.CreateUserProfileParams{
			UserID:      in.ID,
			DisplayName: in.Profile.DisplayName,
			GivenName:   in.Profile.GivenName,
			FamilyName:  in.Profile.FamilyName,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			return fmt.Errorf("create user profile: %w", err)
		}
		identityID, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generate identity id: %w", err)
		}
		if err := queries.CreateExternalIdentity(ctx, gen.CreateExternalIdentityParams{
			ID:        identityID.String(),
			Provider:  in.Provider,
			Subject:   in.Subject,
			Email:     in.Email,
			UserID:    in.ID,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			if db.IsUniqueViolation(err) {
				return domain.ErrExternalIdentityExists
			}
			return fmt.Errorf("create external identity: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("create user with external identity: %w", err)
	}
	return domain.User{
		ID:            in.ID,
		Email:         in.Email,
		EmailVerified: true,
		Role:          domain.RoleUser,
		Status:        domain.StatusActive,
	}, nil
}

// LinkExternalIdentityToUser links a verified identity to the current user in
// one transaction. Conflicts come back as kinds, not errors.
func (r *Repository) LinkExternalIdentityToUser(ctx context.Context, in ports.LinkExternalIdentityInput) (domain.LinkIdentityKind, error) {
	kind := domain.LinkIdentityOK
	err := r.database.InTx(ctx, func(tx db.Tx) error {
		var err error
		kind, err = linkIdentity(ctx, gen.New(tx), in)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("link external identity: %w", err)
	}
	return kind, nil
}

// linkIdentity is the transaction body: user, existing identity, provider
// check, create, and the conditional email verification.
func linkIdentity(ctx context.Context, queries *gen.Queries, in ports.LinkExternalIdentityInput) (domain.LinkIdentityKind, error) {
	now := utctime.Time{Time: in.Now}
	user, err := queries.GetUserForIdentityLink(ctx, in.UserID)
	if errors.Is(err, db.ErrNoRows) {
		return domain.LinkIdentityUserNotFound, nil
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	owner, found, err := identityOwner(ctx, queries, in.Provider, in.Subject)
	if err != nil {
		return "", err
	}
	if found {
		return settleOwnedIdentity(ctx, queries, user, owner, in, now)
	}
	hasProvider, err := hasProviderLink(ctx, queries, in.UserID, in.Provider)
	if err != nil {
		return "", err
	}
	if hasProvider {
		return domain.LinkIdentityProviderAlreadyLinked, nil
	}
	identityID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate identity id: %w", err)
	}
	if err := queries.CreateExternalIdentity(ctx, gen.CreateExternalIdentityParams{
		ID:        identityID.String(),
		Provider:  in.Provider,
		Subject:   in.Subject,
		Email:     in.Email,
		UserID:    in.UserID,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		if !db.IsUniqueViolation(err) {
			return "", fmt.Errorf("create external identity: %w", err)
		}
		return resolveIdentityRace(ctx, queries, user, in, now)
	}
	if err := verifyEmailIfMatching(ctx, queries, user, in.Email, now); err != nil {
		return "", err
	}
	return domain.LinkIdentityOK, nil
}

// identityOwner resolves which user owns a provider identity.
func identityOwner(ctx context.Context, queries *gen.Queries, provider, subject string) (string, bool, error) {
	owner, err := queries.GetExternalIdentityUserID(ctx, gen.GetExternalIdentityUserIDParams{
		Provider: provider,
		Subject:  subject,
	})
	if errors.Is(err, db.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find identity owner: %w", err)
	}
	return owner, true, nil
}

// hasProviderLink reports whether the user already linked this provider.
func hasProviderLink(ctx context.Context, queries *gen.Queries, userID, provider string) (bool, error) {
	_, err := queries.GetExternalIdentityIDByUserAndProvider(ctx, gen.GetExternalIdentityIDByUserAndProviderParams{
		UserID:   userID,
		Provider: provider,
	})
	if errors.Is(err, db.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find provider link: %w", err)
	}
	return true, nil
}

// settleOwnedIdentity handles an identity that already exists: another owner
// is a conflict, the same owner verifies the email and reports already linked.
func settleOwnedIdentity(ctx context.Context, queries *gen.Queries, user gen.GetUserForIdentityLinkRow, owner string, in ports.LinkExternalIdentityInput, now utctime.Time) (domain.LinkIdentityKind, error) {
	if owner != in.UserID {
		return domain.LinkIdentityOwnedByOtherUser, nil
	}
	if err := verifyEmailIfMatching(ctx, queries, user, in.Email, now); err != nil {
		return "", err
	}
	return domain.LinkIdentityAlreadyLinked, nil
}

// resolveIdentityRace re-reads which unique key lost after a create race.
func resolveIdentityRace(ctx context.Context, queries *gen.Queries, user gen.GetUserForIdentityLinkRow, in ports.LinkExternalIdentityInput, now utctime.Time) (domain.LinkIdentityKind, error) {
	owner, found, err := identityOwner(ctx, queries, in.Provider, in.Subject)
	if err != nil {
		return "", err
	}
	if found {
		return settleOwnedIdentity(ctx, queries, user, owner, in, now)
	}
	hasProvider, err := hasProviderLink(ctx, queries, in.UserID, in.Provider)
	if err != nil {
		return "", err
	}
	if hasProvider {
		return domain.LinkIdentityProviderAlreadyLinked, nil
	}
	return "", fmt.Errorf("identity create failed without a conflicting row")
}

// verifyEmailIfMatching marks the account verified when the identity email
// equals the account email and the account is still unverified.
func verifyEmailIfMatching(ctx context.Context, queries *gen.Queries, user gen.GetUserForIdentityLinkRow, email string, now utctime.Time) error {
	if email == "" || !user.EmailVerifiedAt.IsZero() || email != user.Email {
		return nil
	}
	if _, err := queries.VerifyUserEmailIfUnset(ctx, gen.VerifyUserEmailIfUnsetParams{
		EmailVerifiedAt: now,
		UpdatedAt:       now,
		ID:              user.ID,
	}); err != nil {
		return fmt.Errorf("verify user email: %w", err)
	}
	return nil
}

// GetMeView composes the user, profile, and auth methods for Me responses.
func (r *Repository) GetMeView(ctx context.Context, userID string) (domain.MeView, bool, error) {
	row, err := r.queries.GetMeView(ctx, userID)
	if errors.Is(err, db.ErrNoRows) {
		return domain.MeView{}, false, nil
	}
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("get me view: %w", err)
	}
	methods, err := r.queries.GetAuthMethods(ctx, userID)
	if err != nil {
		return domain.MeView{}, false, fmt.Errorf("get auth methods: %w", err)
	}
	view := domain.MeView{
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
	}
	return view, true, nil
}

// orderAuthMethods sorts PASSWORD then GOOGLE. UNION does not order rows.
func orderAuthMethods(methods []string) []string {
	precedence := map[string]int{
		domain.AuthMethodPassword: 0,
		domain.AuthMethodGoogle:   1,
	}
	ordered := slices.Clone(methods)
	slices.SortStableFunc(ordered, func(a, b string) int {
		return precedence[a] - precedence[b]
	})
	return ordered
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
