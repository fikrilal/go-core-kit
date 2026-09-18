package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/app"
	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

var moment = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

const (
	dummyHash      = "hashed:dummy-password-for-timing"
	serviceSession = 30 * 24 * time.Hour
)

type fakeRepository struct {
	created    []ports.CreateUserInput
	sessions   []ports.NewSessionInput
	createErr  error
	loginUser  domain.LoginUser
	loginFound bool
	loginErr   error
}

func (f *fakeRepository) CreateUserWithPassword(_ context.Context, in ports.CreateUserInput) (domain.User, error) {
	if f.createErr != nil {
		return domain.User{}, f.createErr
	}
	f.created = append(f.created, in)
	return domain.User{ID: in.ID, Email: in.Email, Role: domain.RoleUser, Status: domain.StatusActive}, nil
}

func (f *fakeRepository) FindUserForLogin(context.Context, string) (domain.LoginUser, bool, error) {
	return f.loginUser, f.loginFound, f.loginErr
}

func (f *fakeRepository) CreateSession(_ context.Context, in ports.NewSessionInput) error {
	f.sessions = append(f.sessions, in)
	return nil
}

type fakeHasher struct {
	verifiedHash string
	hashErr      error
}

func (f *fakeHasher) Hash(password string) (string, error) {
	if f.hashErr != nil {
		return "", f.hashErr
	}
	return "hashed:" + password, nil
}

func (f *fakeHasher) Verify(encoded, password string) (bool, error) {
	f.verifiedHash = encoded
	return encoded == "hashed:"+password, nil
}

type fakeTokens struct {
	userIDs    []string
	sessionIDs []string
}

func (f *fakeTokens) SignAccessToken(user domain.User, sessionID string) (string, error) {
	f.userIDs = append(f.userIDs, user.ID)
	f.sessionIDs = append(f.sessionIDs, sessionID)
	return "access:" + user.ID, nil
}

type fakeLimiter struct {
	allowedErr error
	failures   int
	successes  int
}

func (f *fakeLimiter) AssertAllowed(context.Context, string, string) error { return f.allowedErr }
func (f *fakeLimiter) RecordFailure(context.Context, string, string) error {
	f.failures++
	return nil
}

func (f *fakeLimiter) RecordSuccess(context.Context, string, string) error {
	f.successes++
	return nil
}

type fixture struct {
	service *app.Service
	repo    *fakeRepository
	hasher  *fakeHasher
	tokens  *fakeTokens
	limiter *fakeLimiter
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		repo:    &fakeRepository{},
		hasher:  &fakeHasher{},
		tokens:  &fakeTokens{},
		limiter: &fakeLimiter{},
	}
	f.service = app.New(
		app.Config{PasswordMinLength: 10, SessionTTL: serviceSession},
		app.Deps{
			Repository: f.repo,
			Hasher:     f.hasher,
			Tokens:     f.tokens,
			Limiter:    f.limiter,
			Clock:      clock.Fixed{Moment: moment},
			DummyHash:  dummyHash,
		},
	)
	return f
}

func problemError(t *testing.T, err error) *httpx.ProblemError {
	t.Helper()
	var problem *httpx.ProblemError
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want a problem error", err)
	}
	return problem
}

func loginFixture(t *testing.T, status string) *fixture {
	t.Helper()
	f := newFixture(t)
	hash := "hashed:" + "correct-password"
	f.repo.loginFound = true
	f.repo.loginUser = domain.LoginUser{
		User:         domain.User{ID: "user-1", Email: "user@example.com", Role: domain.RoleUser, Status: status},
		PasswordHash: &hash,
	}
	return f
}

func TestRegisterPasswordPolicy(t *testing.T) {
	f := newFixture(t)
	_, err := f.service.Register(context.Background(), app.Input{Email: "user@example.com", Password: "short"})
	problem := problemError(t, err)
	if problem.Status != 400 || problem.Code != httpx.CodeValidationFailed {
		t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
	}
	if len(problem.Issues) != 1 || problem.Issues[0].Field != "password" {
		t.Fatalf("issues = %+v, want a password issue", problem.Issues)
	}
	if len(f.repo.created) != 0 {
		t.Error("repository was called for a policy failure")
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	f := newFixture(t)
	f.repo.createErr = domain.ErrEmailExists
	_, err := f.service.Register(context.Background(), app.Input{Email: "user@example.com", Password: "correct-password"})
	problem := problemError(t, err)
	if problem.Status != 409 || problem.Code != httpx.CodeAuthEmailAlreadyExists {
		t.Fatalf("problem = %+v, want 409 AUTH_EMAIL_ALREADY_EXISTS", problem)
	}
}

func TestRegisterIssuesSession(t *testing.T) {
	f := newFixture(t)
	result, err := f.service.Register(context.Background(), app.Input{
		Email: " User@Example.com ", Password: "correct-password", DeviceID: "device-a", DeviceName: "iPhone", IP: "203.0.113.9", UserAgent: "agent",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(f.repo.created) != 1 || f.repo.created[0].Email != "user@example.com" {
		t.Fatalf("created = %+v, want the normalized email", f.repo.created)
	}
	if len(f.repo.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(f.repo.sessions))
	}
	session := f.repo.sessions[0]
	if session.ActiveKey != result.User.ID+":device-a" {
		t.Errorf("active key = %q, want user:device-a", session.ActiveKey)
	}
	if !session.SessionExpiresAt.Equal(moment.Add(serviceSession)) {
		t.Errorf("session expiry = %v, want %v", session.SessionExpiresAt, moment.Add(serviceSession))
	}
	if session.RefreshTokenHash != domain.HashRefreshToken(result.RefreshToken) {
		t.Error("stored refresh token hash does not match the issued token")
	}
	if result.AccessToken != "access:"+result.User.ID || len(f.tokens.sessionIDs) != 1 {
		t.Errorf("tokens = %q/%v, want the signed session token", result.AccessToken, f.tokens.sessionIDs)
	}
	if len(result.AuthMethods) != 1 || result.AuthMethods[0] != domain.AuthMethodPassword {
		t.Errorf("authMethods = %v, want [PASSWORD]", result.AuthMethods)
	}
	if f.limiter.failures != 0 || f.limiter.successes != 0 {
		t.Error("login limiter touched during registration")
	}
}

func TestLoginUsesDummyHashForUnknownEmail(t *testing.T) {
	f := newFixture(t)
	_, err := f.service.Login(context.Background(), app.Input{Email: "ghost@example.com", Password: "correct-password"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthInvalidCredentials {
		t.Fatalf("problem = %+v, want 401 AUTH_INVALID_CREDENTIALS", problem)
	}
	if f.hasher.verifiedHash != dummyHash {
		t.Errorf("verified hash = %q, want the dummy hash (timing equalization)", f.hasher.verifiedHash)
	}
	if f.limiter.failures != 1 {
		t.Errorf("failures = %d, want 1 recorded", f.limiter.failures)
	}
	if len(f.repo.sessions) != 0 {
		t.Error("session issued for an unknown email")
	}
}

func TestLoginWrongPasswordRecordsFailure(t *testing.T) {
	f := loginFixture(t, domain.StatusActive)
	_, err := f.service.Login(context.Background(), app.Input{Email: "user@example.com", Password: "wrong-password"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthInvalidCredentials {
		t.Fatalf("problem = %+v, want 401 AUTH_INVALID_CREDENTIALS", problem)
	}
	if f.hasher.verifiedHash != "hashed:correct-password" {
		t.Errorf("verified hash = %q, want the stored hash", f.hasher.verifiedHash)
	}
	if f.limiter.failures != 1 || f.limiter.successes != 0 {
		t.Errorf("limiter failures/successes = %d/%d, want 1/0", f.limiter.failures, f.limiter.successes)
	}
}

func TestLoginSuspended(t *testing.T) {
	f := loginFixture(t, domain.StatusSuspended)
	_, err := f.service.Login(context.Background(), app.Input{Email: "user@example.com", Password: "correct-password"})
	problem := problemError(t, err)
	if problem.Status != 403 || problem.Code != httpx.CodeAuthUserSuspended {
		t.Fatalf("problem = %+v, want 403 AUTH_USER_SUSPENDED", problem)
	}
	if f.limiter.successes != 0 || len(f.repo.sessions) != 0 {
		t.Error("suspended login recorded success or issued a session")
	}
}

func TestLoginDeletedIsMasked(t *testing.T) {
	f := loginFixture(t, domain.StatusDeleted)
	_, err := f.service.Login(context.Background(), app.Input{Email: "user@example.com", Password: "correct-password"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthInvalidCredentials {
		t.Fatalf("problem = %+v, want masked 401 AUTH_INVALID_CREDENTIALS", problem)
	}
	if f.limiter.failures != 1 {
		t.Errorf("failures = %d, want 1 recorded", f.limiter.failures)
	}
}

func TestLoginRateLimited(t *testing.T) {
	f := loginFixture(t, domain.StatusActive)
	f.limiter.allowedErr = &httpx.ProblemError{Status: 429, Code: httpx.CodeRateLimited, RetryAfterSeconds: 900}
	_, err := f.service.Login(context.Background(), app.Input{Email: "user@example.com", Password: "correct-password"})
	problem := problemError(t, err)
	if problem.Status != 429 || problem.Code != httpx.CodeRateLimited || problem.RetryAfterSeconds != 900 {
		t.Fatalf("problem = %+v, want 429 RATE_LIMITED with retry-after", problem)
	}
	if f.hasher.verifiedHash != "" {
		t.Error("password verified while rate limited; the block must precede the lookup")
	}
}

func TestLoginSuccess(t *testing.T) {
	f := loginFixture(t, domain.StatusActive)
	result, err := f.service.Login(context.Background(), app.Input{Email: "USER@example.com", Password: "correct-password", DeviceID: "device-b"})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if result.User.ID != "user-1" {
		t.Errorf("user id = %q, want user-1", result.User.ID)
	}
	if f.limiter.successes != 1 || f.limiter.failures != 0 {
		t.Errorf("limiter successes/failures = %d/%d, want 1/0", f.limiter.successes, f.limiter.failures)
	}
	if len(f.repo.sessions) != 1 || f.repo.sessions[0].ActiveKey != "user-1:device-b" {
		t.Fatalf("sessions = %+v, want one session with the device active key", f.repo.sessions)
	}
	if result.AccessToken != "access:user-1" {
		t.Errorf("access token = %q, want the signed token", result.AccessToken)
	}
}
