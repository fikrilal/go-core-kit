package app_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	refreshCreds domain.RefreshCredentials
	refreshFound bool
	refreshErr   error
	rotationKind domain.RotationKind
	rotationErr  error
	rotations    []ports.RotateRefreshTokenInput
	revokeResult bool
	revokeErr    error
	revokes      []string

	passwordHash  string
	passwordFound bool
	passwordErr   error
	changeKind    domain.ChangePasswordKind
	changeErr     error
	changes       []ports.ChangePasswordInput

	emailUserID      string
	emailUserIDFound bool
	emailUserIDErr   error
	identityUser     domain.User
	identityFound    bool
	identityErr      error
	createOidcUser   domain.User
	createOidcErr    error
	createOidcInputs []ports.CreateOidcUserInput
	userStatus       string
	userStatusFound  bool
	userStatusErr    error
	linkKind         domain.LinkIdentityKind
	linkErr          error
	links            []ports.LinkExternalIdentityInput
	meView           domain.MeView
	meViewFound      bool
	meViewErr        error
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

func (f *fakeRepository) FindRefreshToken(context.Context, string) (domain.RefreshCredentials, bool, error) {
	return f.refreshCreds, f.refreshFound, f.refreshErr
}

func (f *fakeRepository) RotateRefreshToken(_ context.Context, in ports.RotateRefreshTokenInput) (domain.RotationKind, error) {
	if f.rotationErr != nil {
		return "", f.rotationErr
	}
	f.rotations = append(f.rotations, in)
	return f.rotationKind, nil
}

func (f *fakeRepository) RevokeSessionByRefreshToken(_ context.Context, tokenHash string, _ time.Time) (bool, error) {
	if f.revokeErr != nil {
		return false, f.revokeErr
	}
	f.revokes = append(f.revokes, tokenHash)
	return f.revokeResult, nil
}

func (f *fakeRepository) FindPasswordCredential(context.Context, string) (string, bool, error) {
	return f.passwordHash, f.passwordFound, f.passwordErr
}

func (f *fakeRepository) ChangePasswordAndRevokeOtherSessions(_ context.Context, in ports.ChangePasswordInput) (domain.ChangePasswordKind, error) {
	if f.changeErr != nil {
		return "", f.changeErr
	}
	f.changes = append(f.changes, in)
	return f.changeKind, nil
}

func (f *fakeRepository) FindUserIDByEmail(context.Context, string) (string, bool, error) {
	return f.emailUserID, f.emailUserIDFound, f.emailUserIDErr
}

func (f *fakeRepository) FindUserByExternalIdentity(context.Context, string, string) (domain.User, bool, error) {
	return f.identityUser, f.identityFound, f.identityErr
}

func (f *fakeRepository) CreateUserWithExternalIdentity(_ context.Context, in ports.CreateOidcUserInput) (domain.User, error) {
	f.createOidcInputs = append(f.createOidcInputs, in)
	if f.createOidcErr != nil {
		return domain.User{}, f.createOidcErr
	}
	return f.createOidcUser, nil
}

func (f *fakeRepository) FindUserStatus(context.Context, string) (string, bool, error) {
	return f.userStatus, f.userStatusFound, f.userStatusErr
}

func (f *fakeRepository) LinkExternalIdentityToUser(_ context.Context, in ports.LinkExternalIdentityInput) (domain.LinkIdentityKind, error) {
	if f.linkErr != nil {
		return "", f.linkErr
	}
	f.links = append(f.links, in)
	return f.linkKind, nil
}

func (f *fakeRepository) GetMeView(context.Context, string) (domain.MeView, bool, error) {
	return f.meView, f.meViewFound, f.meViewErr
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
	oidc    *fakeOidc
}

type fakeOidc struct {
	verification domain.OidcVerification
	err          error
	provider     string
	token        string
}

func (f *fakeOidc) VerifyIDToken(_ context.Context, provider, idToken string) (domain.OidcVerification, error) {
	f.provider = provider
	f.token = idToken
	return f.verification, f.err
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		repo:    &fakeRepository{},
		hasher:  &fakeHasher{},
		tokens:  &fakeTokens{},
		limiter: &fakeLimiter{},
		oidc:    &fakeOidc{},
	}
	f.service = app.New(
		app.Config{PasswordMinLength: 10, SessionTTL: serviceSession},
		app.Deps{
			Repository: f.repo,
			Hasher:     f.hasher,
			Tokens:     f.tokens,
			Limiter:    f.limiter,
			Oidc:       f.oidc,
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

func refreshFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.repo.refreshFound = true
	f.repo.refreshCreds = domain.RefreshCredentials{
		User:    domain.User{ID: "user-1", Email: "user@example.com", EmailVerified: true, Role: domain.RoleUser, Status: domain.StatusActive},
		Session: domain.LoginSession{ID: "session-1", ExpiresAt: moment.Add(24 * time.Hour)},
		Token:   domain.RefreshToken{ID: "token-1", ExpiresAt: moment.Add(time.Hour)},
	}
	f.repo.rotationKind = domain.RotationOK
	return f
}

func TestRefreshUnknownToken(t *testing.T) {
	f := newFixture(t)
	f.repo.refreshFound = false
	_, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthRefreshTokenInvalid {
		t.Fatalf("problem = %+v, want 401 AUTH_REFRESH_TOKEN_INVALID", problem)
	}
	if len(f.repo.rotations) != 0 || len(f.tokens.sessionIDs) != 0 {
		t.Error("rotation or signing happened for an unknown token")
	}
}

func TestRefreshReuseContainsFamily(t *testing.T) {
	f := refreshFixture(t)
	f.repo.refreshCreds.Token.Revoked = true
	f.repo.revokeResult = true
	_, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthRefreshTokenReused {
		t.Fatalf("problem = %+v, want 401 AUTH_REFRESH_TOKEN_REUSED", problem)
	}
	if len(f.repo.revokes) != 1 || f.repo.revokes[0] != domain.HashRefreshToken("raw-token") {
		t.Fatalf("reuse containment revokes = %v, want the presented token's family", f.repo.revokes)
	}
	if len(f.repo.rotations) != 0 || len(f.tokens.sessionIDs) != 0 {
		t.Error("reused token was rotated or signed")
	}
}

func TestRefreshExpired(t *testing.T) {
	f := refreshFixture(t)
	f.repo.refreshCreds.Token.ExpiresAt = moment.Add(-time.Minute)
	_, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthRefreshTokenExpired {
		t.Fatalf("problem = %+v, want 401 AUTH_REFRESH_TOKEN_EXPIRED", problem)
	}
}

func TestRefreshSessionRevoked(t *testing.T) {
	f := refreshFixture(t)
	f.repo.refreshCreds.Session.Revoked = true
	_, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	problem := problemError(t, err)
	if problem.Status != 401 || problem.Code != httpx.CodeAuthSessionRevoked {
		t.Fatalf("problem = %+v, want 401 AUTH_SESSION_REVOKED", problem)
	}
}

func TestRefreshSuspendedAndDeleted(t *testing.T) {
	suspended := refreshFixture(t)
	suspended.repo.refreshCreds.User.Status = domain.StatusSuspended
	suspendedResult, suspendedCallErr := suspended.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	if code := problemError(t, mustError(t, suspendedResult, suspendedCallErr)).Code; code != httpx.CodeAuthUserSuspended {
		t.Fatalf("suspended refresh code = %q, want AUTH_USER_SUSPENDED", code)
	}
	deleted := refreshFixture(t)
	deleted.repo.refreshCreds.User.Status = domain.StatusDeleted
	deletedResult, deletedCallErr := deleted.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	if code := problemError(t, mustError(t, deletedResult, deletedCallErr)).Code; code != httpx.CodeAuthRefreshTokenInvalid {
		t.Fatalf("deleted refresh code = %q, want masked AUTH_REFRESH_TOKEN_INVALID", code)
	}
}

func TestRefreshMintsBeforeRotating(t *testing.T) {
	f := refreshFixture(t)
	f.repo.rotationErr = errors.New("rotate exploded")
	_, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
	if err == nil {
		t.Fatal("Refresh() = nil, want the rotation failure")
	}
	if len(f.tokens.sessionIDs) != 1 {
		t.Fatal("access token was not minted before the failing rotation")
	}
}

func TestRefreshSuccess(t *testing.T) {
	f := refreshFixture(t)
	result, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token", IP: "203.0.113.9", UserAgent: "agent"})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(f.repo.rotations) != 1 {
		t.Fatalf("rotations = %d, want 1", len(f.repo.rotations))
	}
	rotation := f.repo.rotations[0]
	if rotation.CurrentTokenHash != domain.HashRefreshToken("raw-token") {
		t.Error("rotation did not receive the presented token hash")
	}
	if rotation.NextTokenHash != domain.HashRefreshToken(result.RefreshToken) {
		t.Error("rotation next-hash does not match the issued token")
	}
	if result.AccessToken != "access:user-1" || len(f.tokens.sessionIDs) != 1 || f.tokens.sessionIDs[0] != "session-1" {
		t.Errorf("tokens = %q/%v, want an access token for session-1", result.AccessToken, f.tokens.sessionIDs)
	}
}

func TestRefreshRotationKindMapping(t *testing.T) {
	cases := map[domain.RotationKind]httpx.Code{
		domain.RotationNotFound:       httpx.CodeAuthRefreshTokenInvalid,
		domain.RotationExpired:        httpx.CodeAuthRefreshTokenExpired,
		domain.RotationSessionRevoked: httpx.CodeAuthSessionRevoked,
		domain.RotationReused:         httpx.CodeAuthRefreshTokenReused,
	}
	for kind, wantCode := range cases {
		t.Run(string(kind), func(t *testing.T) {
			f := refreshFixture(t)
			f.repo.rotationKind = kind
			_, err := f.service.Refresh(context.Background(), app.RefreshInput{RefreshToken: "raw-token"})
			if got := problemError(t, err).Code; got != wantCode {
				t.Fatalf("rotation kind %q mapped to %q, want %q", kind, got, wantCode)
			}
		})
	}
}

func TestLogoutSuccess(t *testing.T) {
	f := newFixture(t)
	f.repo.revokeResult = true
	if err := f.service.Logout(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if len(f.repo.revokes) != 1 || f.repo.revokes[0] != domain.HashRefreshToken("raw-token") {
		t.Fatalf("revokes = %v, want the presented token hash", f.repo.revokes)
	}
}

func TestLogoutUnknownToken(t *testing.T) {
	f := newFixture(t)
	f.repo.revokeResult = false
	problem := problemError(t, f.service.Logout(context.Background(), "raw-token"))
	if problem.Status != 401 || problem.Code != httpx.CodeAuthRefreshTokenInvalid {
		t.Fatalf("problem = %+v, want 401 AUTH_REFRESH_TOKEN_INVALID", problem)
	}
}

func mustError(t *testing.T, result app.Result, err error) error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got result %+v", result)
	}
	return err
}

func changePasswordFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.repo.passwordFound = true
	f.repo.passwordHash = "hashed:correct-password"
	f.repo.changeKind = domain.ChangePasswordOK
	return f
}

func TestChangePasswordPolicy(t *testing.T) {
	f := changePasswordFixture(t)
	err := f.service.ChangePassword(context.Background(), app.ChangePasswordInput{
		UserID: "user-1", SessionID: "session-1", CurrentPassword: "correct-password", NewPassword: "short",
	})
	problem := problemError(t, err)
	if problem.Status != 400 || problem.Code != httpx.CodeValidationFailed {
		t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
	}
	if len(problem.Issues) != 1 || problem.Issues[0].Field != "newPassword" {
		t.Fatalf("issues = %+v, want a newPassword issue", problem.Issues)
	}
	if len(f.repo.changes) != 0 {
		t.Error("repository called for a policy failure")
	}
}

func TestChangePasswordSamePassword(t *testing.T) {
	f := changePasswordFixture(t)
	err := f.service.ChangePassword(context.Background(), app.ChangePasswordInput{
		UserID: "user-1", SessionID: "session-1", CurrentPassword: "repeated-password", NewPassword: "repeated-password",
	})
	problem := problemError(t, err)
	if problem.Status != 400 || problem.Code != httpx.CodeValidationFailed {
		t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
	}
	if len(problem.Issues) != 1 || problem.Issues[0].Field != "newPassword" || problem.Issues[0].Message != "New password must be different" {
		t.Fatalf("issues = %+v, want the different-password issue", problem.Issues)
	}
	if len(f.repo.changes) != 0 {
		t.Error("repository called for identical passwords")
	}
}

func TestChangePasswordNotSet(t *testing.T) {
	f := newFixture(t)
	f.repo.passwordFound = false
	err := f.service.ChangePassword(context.Background(), app.ChangePasswordInput{
		UserID: "user-1", SessionID: "session-1", CurrentPassword: "correct-password", NewPassword: "correct-password-2",
	})
	problem := problemError(t, err)
	if problem.Status != 409 || problem.Code != httpx.CodeAuthPasswordNotSet {
		t.Fatalf("problem = %+v, want 409 AUTH_PASSWORD_NOT_SET", problem)
	}
}

func TestChangePasswordCurrentInvalid(t *testing.T) {
	f := changePasswordFixture(t)
	err := f.service.ChangePassword(context.Background(), app.ChangePasswordInput{
		UserID: "user-1", SessionID: "session-1", CurrentPassword: "wrong-password", NewPassword: "correct-password-2",
	})
	problem := problemError(t, err)
	if problem.Status != 400 || problem.Code != httpx.CodeAuthCurrentPasswordInvalid {
		t.Fatalf("problem = %+v, want 400 AUTH_CURRENT_PASSWORD_INVALID", problem)
	}
	if f.hasher.verifiedHash != "hashed:correct-password" {
		t.Errorf("verified hash = %q, want the stored hash", f.hasher.verifiedHash)
	}
	if len(f.repo.changes) != 0 {
		t.Error("repository called with an invalid current password")
	}
}

func TestChangePasswordSuccess(t *testing.T) {
	f := changePasswordFixture(t)
	err := f.service.ChangePassword(context.Background(), app.ChangePasswordInput{
		UserID: "user-1", SessionID: "session-1", CurrentPassword: "correct-password", NewPassword: "correct-password-2",
	})
	if err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if len(f.repo.changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(f.repo.changes))
	}
	change := f.repo.changes[0]
	if change.UserID != "user-1" || change.SessionID != "session-1" {
		t.Errorf("change user/session = %q/%q, want the principal", change.UserID, change.SessionID)
	}
	if change.ExpectedCurrentPasswordHash != "hashed:correct-password" {
		t.Errorf("expected hash = %q, want the stored hash (compare-and-set)", change.ExpectedCurrentPasswordHash)
	}
	if change.NewPasswordHash != "hashed:correct-password-2" {
		t.Errorf("new hash = %q, want the hashed new password", change.NewPasswordHash)
	}
	if !change.Now.Equal(moment) {
		t.Errorf("now = %v, want the injected clock", change.Now)
	}
}

func exchangeFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.oidc.verification = domain.OidcVerification{
		Kind: domain.OidcVerifyVerified,
		Identity: domain.OidcIdentity{
			Provider:      domain.AuthMethodGoogle,
			Subject:       "google-sub-1",
			Email:         "Dante@Example.com",
			EmailVerified: true,
			DisplayName:   "Dante",
			GivenName:     "Dante",
			FamilyName:    "Alighieri",
		},
	}
	return f
}

func (f *fixture) exchange(t *testing.T) (app.Result, error) {
	t.Helper()
	result, err := f.service.ExchangeOidc(context.Background(), app.OidcExchangeInput{
		Provider: domain.AuthMethodGoogle, IDToken: "id-token", DeviceID: "device-a", UserAgent: "agent",
	})
	if err != nil {
		return app.Result{}, fmt.Errorf("exchange: %w", err)
	}
	return result, nil
}

func TestExchangeOidcCreatesUserAndSession(t *testing.T) {
	f := exchangeFixture(t)
	f.repo.createOidcUser = domain.User{ID: "user-9", Email: "dante@example.com", EmailVerified: true, Role: domain.RoleUser, Status: domain.StatusActive}
	result, err := f.exchange(t)
	if err != nil {
		t.Fatalf("ExchangeOidc() error = %v", err)
	}
	if result.User.ID != "user-9" {
		t.Errorf("user = %q, want user-9", result.User.ID)
	}
	if len(f.repo.createOidcInputs) != 1 {
		t.Fatalf("create inputs = %d, want 1", len(f.repo.createOidcInputs))
	}
	created := f.repo.createOidcInputs[0]
	if created.Email != "dante@example.com" || created.Provider != domain.AuthMethodGoogle || created.Subject != "google-sub-1" {
		t.Errorf("created = %+v, want normalized email and the verified identity", created)
	}
	if created.Profile.DisplayName != "Dante" || created.Profile.FamilyName != "Alighieri" {
		t.Errorf("profile = %+v, want the token names", created.Profile)
	}
	if created.ID == "" {
		t.Error("created user id is empty")
	}
	if len(f.repo.sessions) != 1 || len(f.tokens.sessionIDs) != 1 {
		t.Errorf("sessions/tokens = %d/%d, want one issued session", len(f.repo.sessions), len(f.tokens.sessionIDs))
	}
}

func TestExchangeOidcExistingIdentityReusesUser(t *testing.T) {
	f := exchangeFixture(t)
	f.repo.identityFound = true
	f.repo.identityUser = domain.User{ID: "user-5", Email: "dante@example.com", EmailVerified: true, Role: domain.RoleUser, Status: domain.StatusActive}
	result, err := f.exchange(t)
	if err != nil {
		t.Fatalf("ExchangeOidc() error = %v", err)
	}
	if result.User.ID != "user-5" {
		t.Errorf("user = %q, want the existing user", result.User.ID)
	}
	if len(f.repo.createOidcInputs) != 0 {
		t.Error("user created for an existing identity")
	}
}

func TestExchangeOidcIdentityRaceRefetchesWinner(t *testing.T) {
	f := exchangeFixture(t)
	f.repo.createOidcErr = domain.ErrExternalIdentityExists
	f.repo.identityFound = true
	f.repo.identityUser = domain.User{ID: "user-7", Email: "dante@example.com", Role: domain.RoleUser, Status: domain.StatusActive}
	result, err := f.exchange(t)
	if err != nil {
		t.Fatalf("ExchangeOidc() error = %v", err)
	}
	if result.User.ID != "user-7" {
		t.Errorf("user = %q, want the raced winner", result.User.ID)
	}
}

func TestExchangeOidcLinkRequired(t *testing.T) {
	f := exchangeFixture(t)
	f.repo.emailUserIDFound = true
	f.repo.emailUserID = "existing-user"
	_, err := f.exchange(t)
	problem := problemError(t, err)
	if problem.Status != 409 || problem.Code != httpx.CodeAuthOidcLinkRequired {
		t.Fatalf("problem = %+v, want 409 AUTH_OIDC_LINK_REQUIRED", problem)
	}
	if len(f.repo.createOidcInputs) != 0 {
		t.Error("user created despite the email collision")
	}
}

func TestExchangeOidcKindMapping(t *testing.T) {
	cases := map[domain.OidcVerifyKind]struct {
		status int
		code   httpx.Code
	}{
		domain.OidcVerifyNotConfigured: {status: 500, code: httpx.CodeAuthOidcNotConfigured},
		domain.OidcVerifyInvalid:       {status: 401, code: httpx.CodeAuthOidcTokenInvalid},
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			f := exchangeFixture(t)
			f.oidc.verification = domain.OidcVerification{Kind: kind}
			_, err := f.exchange(t)
			problem := problemError(t, err)
			if problem.Status != want.status || problem.Code != want.code {
				t.Fatalf("kind %q mapped to %d/%s, want %d/%s", kind, problem.Status, problem.Code, want.status, want.code)
			}
		})
	}
}

func TestExchangeOidcUnexpectedKind(t *testing.T) {
	f := exchangeFixture(t)
	f.oidc.verification = domain.OidcVerification{Kind: "mystery"}
	_, err := f.service.ExchangeOidc(context.Background(), app.OidcExchangeInput{
		Provider: domain.AuthMethodGoogle, IDToken: "id-token",
	})
	if err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("ExchangeOidc() error = %v, want the unexpected kind", err)
	}
	var problem *httpx.ProblemError
	if errors.As(err, &problem) {
		t.Fatalf("unexpected kind became a problem: %+v", problem)
	}
}

func TestExchangeOidcEmailNotVerified(t *testing.T) {
	f := exchangeFixture(t)
	f.oidc.verification.Identity.EmailVerified = false
	_, err := f.exchange(t)
	problem := problemError(t, err)
	if problem.Status != 400 || problem.Code != httpx.CodeAuthOidcEmailNotVerified {
		t.Fatalf("problem = %+v, want 400 AUTH_OIDC_EMAIL_NOT_VERIFIED", problem)
	}
}

func TestExchangeOidcDeletedAndSuspended(t *testing.T) {
	deleted := exchangeFixture(t)
	deleted.repo.identityFound = true
	deleted.repo.identityUser = domain.User{ID: "user-1", Status: domain.StatusDeleted}
	deletedResult, deletedCallErr := deleted.exchange(t)
	if code := problemError(t, mustError(t, deletedResult, deletedCallErr)).Code; code != httpx.CodeAuthInvalidCredentials {
		t.Fatalf("deleted exchange code = %q, want masked AUTH_INVALID_CREDENTIALS", code)
	}
	suspended := exchangeFixture(t)
	suspended.repo.identityFound = true
	suspended.repo.identityUser = domain.User{ID: "user-1", Status: domain.StatusSuspended}
	suspendedResult, suspendedCallErr := suspended.exchange(t)
	if code := problemError(t, mustError(t, suspendedResult, suspendedCallErr)).Code; code != httpx.CodeAuthUserSuspended {
		t.Fatalf("suspended exchange code = %q, want AUTH_USER_SUSPENDED", code)
	}
}

func connectFixture(t *testing.T) *fixture {
	t.Helper()
	f := exchangeFixture(t)
	f.repo.userStatusFound = true
	f.repo.userStatus = domain.StatusActive
	f.repo.linkKind = domain.LinkIdentityOK
	return f
}

func (f *fixture) connect() error {
	err := f.service.ConnectOidc(context.Background(), app.OidcConnectInput{
		UserID: "user-1", Provider: domain.AuthMethodGoogle, IDToken: "id-token",
	})
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}

func TestConnectOidcLinksIdentity(t *testing.T) {
	f := connectFixture(t)
	if err := f.connect(); err != nil {
		t.Fatalf("ConnectOidc() error = %v", err)
	}
	if len(f.repo.links) != 1 {
		t.Fatalf("links = %d, want 1", len(f.repo.links))
	}
	link := f.repo.links[0]
	if link.UserID != "user-1" || link.Provider != domain.AuthMethodGoogle || link.Subject != "google-sub-1" {
		t.Errorf("link = %+v, want the verified identity for the principal", link)
	}
	if link.Email != "dante@example.com" {
		t.Errorf("link email = %q, want the normalized identity email", link.Email)
	}
	if !link.Now.Equal(moment) {
		t.Errorf("link now = %v, want the injected clock", link.Now)
	}
}

func TestConnectOidcAlreadyLinkedIsOK(t *testing.T) {
	f := connectFixture(t)
	f.repo.linkKind = domain.LinkIdentityAlreadyLinked
	if err := f.connect(); err != nil {
		t.Fatalf("ConnectOidc() error = %v, want nil for already_linked", err)
	}
}

func TestConnectOidcKindMapping(t *testing.T) {
	cases := map[domain.LinkIdentityKind]struct {
		status int
		code   httpx.Code
	}{
		domain.LinkIdentityUserNotFound:          {status: 401, code: httpx.CodeUnauthorized},
		domain.LinkIdentityOwnedByOtherUser:      {status: 409, code: httpx.CodeAuthOidcIdentityLinked},
		domain.LinkIdentityProviderAlreadyLinked: {status: 409, code: httpx.CodeAuthOidcProviderLinked},
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			f := connectFixture(t)
			f.repo.linkKind = kind
			problem := problemError(t, f.connect())
			if problem.Status != want.status || problem.Code != want.code {
				t.Fatalf("kind %q mapped to %d/%s, want %d/%s", kind, problem.Status, problem.Code, want.status, want.code)
			}
		})
	}
}

func TestConnectOidcUnexpectedLinkKind(t *testing.T) {
	f := connectFixture(t)
	f.repo.linkKind = "mystery"
	err := f.connect()
	if err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("ConnectOidc() error = %v, want the unexpected kind", err)
	}
	var problem *httpx.ProblemError
	if errors.As(err, &problem) {
		t.Fatalf("unexpected kind became a problem: %+v", problem)
	}
}

func TestConnectOidcMissingAndDeletedUsersAreUnauthorized(t *testing.T) {
	missing := connectFixture(t)
	missing.repo.userStatusFound = false
	problem := problemError(t, missing.connect())
	if problem.Status != 401 || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("missing user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
	if missing.oidc.token != "" {
		t.Error("token verified for a missing user; the status check must come first")
	}

	deleted := connectFixture(t)
	deleted.repo.userStatus = domain.StatusDeleted
	problem = problemError(t, deleted.connect())
	if problem.Status != 401 || problem.Code != httpx.CodeUnauthorized {
		t.Fatalf("deleted user mapped to %d/%s, want 401 UNAUTHORIZED", problem.Status, problem.Code)
	}
	if deleted.oidc.token != "" {
		t.Error("token verified for a deleted user; the status check must come first")
	}
}

func TestConnectOidcSuspendedUserProceeds(t *testing.T) {
	f := connectFixture(t)
	f.repo.userStatus = domain.StatusSuspended
	if err := f.connect(); err != nil {
		t.Fatalf("ConnectOidc() error = %v, want a suspended user to connect (reference parity)", err)
	}
	if len(f.repo.links) != 1 {
		t.Error("identity not linked for the suspended user")
	}
}

func TestConnectOidcVerifierKindMapping(t *testing.T) {
	cases := map[domain.OidcVerifyKind]struct {
		status int
		code   httpx.Code
	}{
		domain.OidcVerifyNotConfigured: {status: 500, code: httpx.CodeAuthOidcNotConfigured},
		domain.OidcVerifyInvalid:       {status: 401, code: httpx.CodeAuthOidcTokenInvalid},
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			f := connectFixture(t)
			f.oidc.verification = domain.OidcVerification{Kind: kind}
			problem := problemError(t, f.connect())
			if problem.Status != want.status || problem.Code != want.code {
				t.Fatalf("kind %q mapped to %d/%s, want %d/%s", kind, problem.Status, problem.Code, want.status, want.code)
			}
		})
	}
	t.Run("email not verified", func(t *testing.T) {
		f := connectFixture(t)
		f.oidc.verification.Identity.EmailVerified = false
		problem := problemError(t, f.connect())
		if problem.Status != 400 || problem.Code != httpx.CodeAuthOidcEmailNotVerified {
			t.Fatalf("unverified email mapped to %d/%s, want 400 AUTH_OIDC_EMAIL_NOT_VERIFIED", problem.Status, problem.Code)
		}
	})
}

func TestMeViewPassesThroughRepository(t *testing.T) {
	f := newFixture(t)
	f.repo.meViewFound = true
	f.repo.meView = domain.MeView{
		User:        domain.User{ID: "user-1", Email: "user@example.com", Role: domain.RoleUser, Status: domain.StatusActive},
		Profile:     domain.Profile{DisplayName: "Dante"},
		AuthMethods: []string{domain.AuthMethodGoogle, domain.AuthMethodPassword},
	}
	view, err := f.service.MeView(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("MeView() error = %v", err)
	}
	if view.User.ID != "user-1" || view.Profile.DisplayName != "Dante" || len(view.AuthMethods) != 2 {
		t.Fatalf("view = %+v, want the repository view", view)
	}
	f.repo.meViewFound = false
	if _, err := f.service.MeView(context.Background(), "missing"); err == nil {
		t.Fatal("MeView() = nil for a missing user, want an error")
	}
}

func TestChangePasswordKindMapping(t *testing.T) {
	cases := map[domain.ChangePasswordKind]struct {
		status int
		code   httpx.Code
	}{
		domain.ChangePasswordNotFound:                {status: 401, code: httpx.CodeUnauthorized},
		domain.ChangePasswordPasswordNotSet:          {status: 409, code: httpx.CodeAuthPasswordNotSet},
		domain.ChangePasswordCurrentPasswordMismatch: {status: 400, code: httpx.CodeAuthCurrentPasswordInvalid},
	}
	for kind, want := range cases {
		t.Run(string(kind), func(t *testing.T) {
			f := changePasswordFixture(t)
			f.repo.changeKind = kind
			err := f.service.ChangePassword(context.Background(), app.ChangePasswordInput{
				UserID: "user-1", SessionID: "session-1", CurrentPassword: "correct-password", NewPassword: "correct-password-2",
			})
			problem := problemError(t, err)
			if problem.Status != want.status || problem.Code != want.code {
				t.Fatalf("kind %q mapped to %d/%s, want %d/%s", kind, problem.Status, problem.Code, want.status, want.code)
			}
		})
	}
}
