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
		return Result{}, &httpx.ProblemError{
			Status: http.StatusForbidden,
			Code:   httpx.CodeAuthUserSuspended,
			Detail: "User is suspended",
		}
	}
	if err := s.limiter.RecordSuccess(ctx, email, in.IP); err != nil {
		return Result{}, fmt.Errorf("auth login: record success: %w", err)
	}
	return s.issueSession(ctx, found.User, []string{domain.AuthMethodPassword}, in, s.clock.Now())
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
