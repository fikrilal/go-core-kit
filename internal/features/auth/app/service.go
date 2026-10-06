// Package app implements the auth feature's use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// Config carries the auth policy knobs.
type Config struct {
	PasswordMinLength int
	SessionTTL        time.Duration
}

// Deps are the service's collaborators.
type Deps struct {
	Repository ports.Repository
	Hasher     ports.PasswordHasher
	Tokens     ports.AccessTokens
	Limiter    ports.LoginLimiter
	Oidc       ports.OidcVerifier
	Clock      clock.Clock
	DummyHash  string
}

// Service implements the auth use cases.
type Service struct {
	repo      ports.Repository
	hasher    ports.PasswordHasher
	tokens    ports.AccessTokens
	limiter   ports.LoginLimiter
	oidc      ports.OidcVerifier
	clock     clock.Clock
	config    Config
	dummyHash string
}

// New builds the auth service.
func New(config Config, deps Deps) *Service {
	return &Service{
		repo:      deps.Repository,
		hasher:    deps.Hasher,
		tokens:    deps.Tokens,
		limiter:   deps.Limiter,
		oidc:      deps.Oidc,
		clock:     deps.Clock,
		config:    config,
		dummyHash: deps.DummyHash,
	}
}

// Input carries a register or login request.
type Input struct {
	Email      string
	Password   string
	DeviceID   string
	DeviceName string
	IP         string
	UserAgent  string
}

// SessionParams carry client metadata for session issuance.
type SessionParams struct {
	DeviceID   string
	DeviceName string
	IP         string
	UserAgent  string
}

// Result is the issued session a handler renders.
type Result struct {
	User         domain.User
	AccessToken  string
	RefreshToken string
}

// Register creates a password account and issues tokens.
func (s *Service) Register(ctx context.Context, in Input) (Result, error) {
	email := domain.NormalizeEmail(in.Email)
	if err := s.checkPasswordPolicy(in.Password, "password"); err != nil {
		return Result{}, err
	}
	passwordHash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return Result{}, fmt.Errorf("auth register: hash password: %w", err)
	}
	userID, err := uuid.NewV7()
	if err != nil {
		return Result{}, fmt.Errorf("auth register: generate user id: %w", err)
	}
	now := s.clock.Now()
	user, err := s.repo.CreateUserWithPassword(ctx, ports.CreateUserInput{
		ID:           userID.String(),
		Email:        email,
		PasswordHash: passwordHash,
		Now:          now,
	})
	if errors.Is(err, domain.ErrEmailExists) {
		return Result{}, emailAlreadyExistsError()
	}
	if err != nil {
		return Result{}, fmt.Errorf("auth register: create user: %w", err)
	}
	return s.issueSession(ctx, user, sessionParams(in), now)
}

func sessionParams(in Input) SessionParams {
	return SessionParams{DeviceID: in.DeviceID, DeviceName: in.DeviceName, IP: in.IP, UserAgent: in.UserAgent}
}

// Login verifies credentials and issues tokens.
// A deleted user shares the invalid-credentials path on purpose.
func (s *Service) Login(ctx context.Context, in Input) (Result, error) {
	email := domain.NormalizeEmail(in.Email)
	if err := s.limiter.AssertAllowed(ctx, email, in.IP); err != nil {
		return Result{}, fmt.Errorf("auth login: rate limit: %w", err)
	}
	found, ok, err := s.repo.FindUserForLogin(ctx, email)
	if err != nil {
		return Result{}, fmt.Errorf("auth login: find user: %w", err)
	}
	passwordHash := s.dummyHash
	if ok && found.PasswordHash != nil {
		passwordHash = *found.PasswordHash
	}
	verified, err := s.hasher.Verify(passwordHash, in.Password)
	if err != nil {
		return Result{}, fmt.Errorf("auth login: verify password: %w", err)
	}
	if !ok || !verified || found.User.Status == domain.StatusDeleted {
		if err := s.limiter.RecordFailure(ctx, email, in.IP); err != nil {
			return Result{}, fmt.Errorf("auth login: record failure: %w", err)
		}
		return Result{}, invalidCredentials()
	}
	if found.User.Status == domain.StatusSuspended {
		return Result{}, suspendedError()
	}
	if err := s.limiter.RecordSuccess(ctx, email, in.IP); err != nil {
		return Result{}, fmt.Errorf("auth login: record success: %w", err)
	}
	return s.issueSession(ctx, found.User, sessionParams(in), s.clock.Now())
}

// RefreshInput carries a token refresh.
type RefreshInput struct {
	RefreshToken string
	IP           string
	UserAgent    string
}

// Refresh rotates a refresh token and issues a new token pair.
// The first checks and rotateExisting are the same outcomes. The transaction wins a race.
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (Result, error) {
	now := s.clock.Now()
	currentHash := domain.HashRefreshToken(in.RefreshToken)
	found, ok, err := s.repo.FindRefreshToken(ctx, currentHash)
	if err != nil {
		return Result{}, fmt.Errorf("auth refresh: find token: %w", err)
	}
	if !ok || found.User.Status == domain.StatusDeleted {
		return Result{}, invalidRefreshToken()
	}
	if found.User.Status == domain.StatusSuspended {
		return Result{}, suspendedError()
	}
	if found.Session.Revoked {
		return Result{}, sessionRevokedError()
	}
	if !found.Token.ExpiresAt.After(now) || !found.Session.ExpiresAt.After(now) {
		return Result{}, refreshTokenExpiredError()
	}
	if found.Token.Revoked || found.Token.ReplacedByID != nil {
		if _, err := s.repo.RevokeSessionByRefreshToken(ctx, currentHash, now); err != nil {
			return Result{}, fmt.Errorf("auth refresh: contain reuse: %w", err)
		}
		return Result{}, refreshTokenReusedError()
	}

	// Mint the access token before rotating so a failing refresh never consumes
	// the caller's refresh token.
	accessToken, err := s.tokens.SignAccessToken(found.User, found.Session.ID)
	if err != nil {
		return Result{}, fmt.Errorf("auth refresh: issue access token: %w", err)
	}
	nextToken, err := domain.NewRefreshToken()
	if err != nil {
		return Result{}, fmt.Errorf("auth refresh: create refresh token: %w", err)
	}
	nextID, err := uuid.NewV7()
	if err != nil {
		return Result{}, fmt.Errorf("auth refresh: generate token id: %w", err)
	}
	userAgent, _ := domain.NormalizeUserAgent(in.UserAgent)
	kind, err := s.repo.RotateRefreshToken(ctx, ports.RotateRefreshTokenInput{
		CurrentTokenHash: currentHash,
		NextTokenID:      nextID.String(),
		NextTokenHash:    domain.HashRefreshToken(nextToken),
		Now:              now,
		IP:               in.IP,
		UserAgent:        userAgent,
	})
	if err != nil {
		return Result{}, fmt.Errorf("auth refresh: rotate token: %w", err)
	}
	switch kind {
	case domain.RotationOK:
		return Result{User: found.User, AccessToken: accessToken, RefreshToken: nextToken}, nil
	case domain.RotationNotFound:
		return Result{}, invalidRefreshToken()
	case domain.RotationExpired:
		return Result{}, refreshTokenExpiredError()
	case domain.RotationSessionRevoked:
		return Result{}, sessionRevokedError()
	case domain.RotationReused:
		return Result{}, refreshTokenReusedError()
	}
	return Result{}, fmt.Errorf("auth refresh: unexpected rotation result %q", kind)
}

// Logout revokes the session behind a refresh token.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	revoked, err := s.repo.RevokeSessionByRefreshToken(ctx, domain.HashRefreshToken(refreshToken), s.clock.Now())
	if err != nil {
		return fmt.Errorf("auth logout: %w", err)
	}
	if !revoked {
		return invalidRefreshToken()
	}
	return nil
}

// ChangePasswordInput carries a password change for the current user.
type ChangePasswordInput struct {
	UserID          string
	SessionID       string
	CurrentPassword string
	NewPassword     string
}

// OidcExchangeInput carries an OIDC exchange request.
type OidcExchangeInput struct {
	Provider   string
	IDToken    string
	DeviceID   string
	DeviceName string
	IP         string
	UserAgent  string
}

// ExchangeOidc verifies an OIDC id token and issues a session for the resolved user.
// A deleted OIDC user uses that same invalid-credentials path on purpose.
func (s *Service) ExchangeOidc(ctx context.Context, in OidcExchangeInput) (Result, error) {
	identity, err := s.verifyOidcIdentity(ctx, in.Provider, in.IDToken)
	if err != nil {
		return Result{}, err
	}
	now := s.clock.Now()
	email := domain.NormalizeEmail(identity.Email)
	user, found, err := s.repo.FindUserByExternalIdentity(ctx, identity.Provider, identity.Subject)
	if err != nil {
		return Result{}, fmt.Errorf("auth oidc: find identity: %w", err)
	}
	if !found {
		user, err = s.createOidcUser(ctx, identity, email, now)
		if err != nil {
			return Result{}, err
		}
	}
	if user.Status == domain.StatusDeleted {
		return Result{}, invalidCredentials()
	}
	if user.Status == domain.StatusSuspended {
		return Result{}, suspendedError()
	}
	return s.issueSession(ctx, user, SessionParams{
		DeviceID:   in.DeviceID,
		DeviceName: in.DeviceName,
		IP:         in.IP,
		UserAgent:  in.UserAgent,
	}, now)
}

// verifyOidcIdentity verifies an id_token and maps the three failures both
// OIDC flows return. Verified continues into the caller's own checks.
func (s *Service) verifyOidcIdentity(ctx context.Context, provider, idToken string) (domain.OidcIdentity, error) {
	verification, err := s.oidc.VerifyIDToken(ctx, provider, idToken)
	if err != nil {
		return domain.OidcIdentity{}, fmt.Errorf("auth oidc: verify token: %w", err)
	}
	switch verification.Kind {
	case domain.OidcVerifyNotConfigured:
		return domain.OidcIdentity{}, oidcNotConfiguredError()
	case domain.OidcVerifyInvalid:
		return domain.OidcIdentity{}, oidcTokenInvalidError()
	case domain.OidcVerifyVerified:
		// Verified continues into the caller's checks.
	default:
		return domain.OidcIdentity{}, fmt.Errorf("auth oidc: unexpected verification kind %q", verification.Kind)
	}
	if !verification.Identity.EmailVerified {
		return domain.OidcIdentity{}, oidcEmailNotVerifiedError()
	}
	return verification.Identity, nil
}

// OidcConnectInput carries an identity link for the authenticated user.
type OidcConnectInput struct {
	UserID   string
	Provider string
	IDToken  string
}

// ConnectOidc links a verified OIDC identity to the authenticated user.
// Only a deleted account is rejected up front; suspended accounts may connect.
func (s *Service) ConnectOidc(ctx context.Context, in OidcConnectInput) error {
	status, found, err := s.repo.FindUserStatus(ctx, in.UserID)
	if err != nil {
		return fmt.Errorf("auth oidc connect: find user: %w", err)
	}
	if !found || status == domain.StatusDeleted {
		return unauthorizedError()
	}
	identity, err := s.verifyOidcIdentity(ctx, in.Provider, in.IDToken)
	if err != nil {
		return err
	}
	kind, err := s.repo.LinkExternalIdentityToUser(ctx, ports.LinkExternalIdentityInput{
		UserID:   in.UserID,
		Provider: identity.Provider,
		Subject:  identity.Subject,
		Email:    domain.NormalizeEmail(identity.Email),
		Now:      s.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("auth oidc connect: link identity: %w", err)
	}
	switch kind {
	case domain.LinkIdentityOK, domain.LinkIdentityAlreadyLinked:
		return nil
	case domain.LinkIdentityUserNotFound:
		return unauthorizedError()
	case domain.LinkIdentityOwnedByOtherUser:
		return oidcIdentityAlreadyLinkedError()
	case domain.LinkIdentityProviderAlreadyLinked:
		return oidcProviderAlreadyLinkedError()
	}
	return fmt.Errorf("auth oidc connect: unexpected link result %q", kind)
}

func (s *Service) createOidcUser(ctx context.Context, identity domain.OidcIdentity, email string, now time.Time) (domain.User, error) {
	_, exists, err := s.repo.FindUserIDByEmail(ctx, email)
	if err != nil {
		return domain.User{}, fmt.Errorf("auth oidc: find email: %w", err)
	}
	if exists {
		return domain.User{}, oidcLinkRequiredError()
	}
	userID, err := uuid.NewV7()
	if err != nil {
		return domain.User{}, fmt.Errorf("auth oidc: generate user id: %w", err)
	}
	user, err := s.repo.CreateUserWithExternalIdentity(ctx, ports.CreateOidcUserInput{
		ID:    userID.String(),
		Email: email,
		Profile: domain.Profile{
			DisplayName: identity.DisplayName,
			GivenName:   identity.GivenName,
			FamilyName:  identity.FamilyName,
		},
		Provider: identity.Provider,
		Subject:  identity.Subject,
		Now:      now,
	})
	if errors.Is(err, domain.ErrEmailExists) {
		return domain.User{}, oidcLinkRequiredError()
	}
	if errors.Is(err, domain.ErrExternalIdentityExists) {
		existing, found, err := s.repo.FindUserByExternalIdentity(ctx, identity.Provider, identity.Subject)
		if err != nil {
			return domain.User{}, fmt.Errorf("auth oidc: refetch identity: %w", err)
		}
		if !found {
			return domain.User{}, fmt.Errorf("auth oidc: identity missing after conflict")
		}
		return existing, nil
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("auth oidc: create user: %w", err)
	}
	return user, nil
}

// MeView composes the user, profile, and auth methods for Me responses.
func (s *Service) MeView(ctx context.Context, userID string) (domain.MeView, error) {
	view, found, err := s.repo.GetMeView(ctx, userID)
	if err != nil {
		return domain.MeView{}, fmt.Errorf("auth me view: %w", err)
	}
	if !found {
		return domain.MeView{}, fmt.Errorf("auth me view: user %s not found", userID)
	}
	return view, nil
}

// ChangePassword changes the authenticated user's password and revokes other sessions.
func (s *Service) ChangePassword(ctx context.Context, in ChangePasswordInput) error {
	if err := s.checkPasswordPolicy(in.NewPassword, "newPassword"); err != nil {
		return err
	}
	if in.CurrentPassword == in.NewPassword {
		return newPasswordMustDifferError()
	}
	existingHash, found, err := s.repo.FindPasswordCredential(ctx, in.UserID)
	if err != nil {
		return fmt.Errorf("auth change password: find credential: %w", err)
	}
	if !found {
		return passwordNotSetError()
	}
	verified, err := s.hasher.Verify(existingHash, in.CurrentPassword)
	if err != nil {
		return fmt.Errorf("auth change password: verify current password: %w", err)
	}
	if !verified {
		return currentPasswordInvalidError()
	}
	newHash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return fmt.Errorf("auth change password: hash password: %w", err)
	}
	kind, err := s.repo.ChangePasswordAndRevokeOtherSessions(ctx, ports.ChangePasswordInput{
		UserID:                      in.UserID,
		SessionID:                   in.SessionID,
		ExpectedCurrentPasswordHash: existingHash,
		NewPasswordHash:             newHash,
		Now:                         s.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("auth change password: update: %w", err)
	}
	switch kind {
	case domain.ChangePasswordOK:
		return nil
	case domain.ChangePasswordNotFound:
		return unauthorizedError()
	case domain.ChangePasswordPasswordNotSet:
		return passwordNotSetError()
	case domain.ChangePasswordCurrentPasswordMismatch:
		return currentPasswordInvalidError()
	}
	return fmt.Errorf("auth change password: unexpected result %q", kind)
}

// issueSession stores the session, then signs. SessionTTL is both the session expiry and the refresh-token expiry.
func (s *Service) issueSession(ctx context.Context, user domain.User, session SessionParams, now time.Time) (Result, error) {
	refreshToken, err := domain.NewRefreshToken()
	if err != nil {
		return Result{}, fmt.Errorf("auth: create refresh token: %w", err)
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return Result{}, fmt.Errorf("auth: generate session id: %w", err)
	}
	refreshTokenID, err := uuid.NewV7()
	if err != nil {
		return Result{}, fmt.Errorf("auth: generate refresh token id: %w", err)
	}
	activeKey, _ := domain.ActiveSessionKey(user.ID, session.DeviceID)
	userAgent, _ := domain.NormalizeUserAgent(session.UserAgent)
	if err := s.repo.CreateSession(ctx, ports.NewSessionInput{
		SessionID:        sessionID.String(),
		RefreshTokenID:   refreshTokenID.String(),
		RefreshTokenHash: domain.HashRefreshToken(refreshToken),
		UserID:           user.ID,
		DeviceID:         session.DeviceID,
		DeviceName:       session.DeviceName,
		IP:               session.IP,
		UserAgent:        userAgent,
		ActiveKey:        activeKey,
		Now:              now,
		SessionExpiresAt: now.Add(s.config.SessionTTL),
	}); err != nil {
		return Result{}, fmt.Errorf("auth: create session: %w", err)
	}
	accessToken, err := s.tokens.SignAccessToken(user, sessionID.String())
	if err != nil {
		return Result{}, fmt.Errorf("auth: issue session: %w", err)
	}
	return Result{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *Service) checkPasswordPolicy(password, field string) error {
	if utf8.RuneCountInString(password) >= s.config.PasswordMinLength {
		return nil
	}
	return passwordTooShortError(field, s.config.PasswordMinLength)
}
