// Package app implements the auth feature's use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
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
	Clock      clock.Clock
	DummyHash  string
}

// Service implements password register and login.
type Service struct {
	repo      ports.Repository
	hasher    ports.PasswordHasher
	tokens    ports.AccessTokens
	limiter   ports.LoginLimiter
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

// Result is the issued session a handler renders.
type Result struct {
	User         domain.User
	AuthMethods  []string
	AccessToken  string
	RefreshToken string
}

// Register creates a password account and issues tokens.
func (s *Service) Register(ctx context.Context, in Input) (Result, error) {
	email := domain.NormalizeEmail(in.Email)
	if err := s.checkPasswordPolicy(in.Password); err != nil {
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
		return Result{}, &httpx.ProblemError{
			Status: http.StatusConflict,
			Code:   httpx.CodeAuthEmailAlreadyExists,
			Detail: "Email already exists",
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("auth register: create user: %w", err)
	}
	return s.issueSession(ctx, user, []string{domain.AuthMethodPassword}, in, now)
}

// Login verifies credentials and issues tokens.
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
	return s.issueSession(ctx, found.User, []string{domain.AuthMethodPassword}, in, s.clock.Now())
}

// RefreshInput carries a token refresh.
type RefreshInput struct {
	RefreshToken string
	IP           string
	UserAgent    string
}

// Refresh rotates a refresh token and issues a new token pair.
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

func (s *Service) issueSession(ctx context.Context, user domain.User, methods []string, in Input, now time.Time) (Result, error) {
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
	activeKey, _ := domain.ActiveSessionKey(user.ID, in.DeviceID)
	userAgent, _ := domain.NormalizeUserAgent(in.UserAgent)
	if err := s.repo.CreateSession(ctx, ports.NewSessionInput{
		SessionID:        sessionID.String(),
		RefreshTokenID:   refreshTokenID.String(),
		RefreshTokenHash: domain.HashRefreshToken(refreshToken),
		UserID:           user.ID,
		DeviceID:         in.DeviceID,
		DeviceName:       in.DeviceName,
		IP:               in.IP,
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
		AuthMethods:  methods,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *Service) checkPasswordPolicy(password string) error {
	if utf8.RuneCountInString(password) >= s.config.PasswordMinLength {
		return nil
	}
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeValidationFailed,
		Issues: []httpx.FieldIssue{{
			Field:   "password",
			Message: fmt.Sprintf("Password must be at least %d characters", s.config.PasswordMinLength),
		}},
	}
}

func invalidCredentials() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthInvalidCredentials,
		Detail: "Invalid credentials",
	}
}

func invalidRefreshToken() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthRefreshTokenInvalid,
		Detail: "Invalid refresh token",
	}
}

func refreshTokenExpiredError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthRefreshTokenExpired,
		Detail: "Refresh token expired",
	}
}

func refreshTokenReusedError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthRefreshTokenReused,
		Detail: "Refresh token reuse detected",
	}
}

func sessionRevokedError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthSessionRevoked,
		Detail: "Session revoked",
	}
}

func suspendedError() error {
	return &httpx.ProblemError{
		Status: http.StatusForbidden,
		Code:   httpx.CodeAuthUserSuspended,
		Detail: "User is suspended",
	}
}
