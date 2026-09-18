//go:build !pg

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

const (
	testPassword = "correct-horse-battery-staple"
	testKid      = "test-kid"
)

type authFixture struct {
	suite    *contracttest.Suite
	client   *http.Client
	baseURL  string
	database *db.Database
	cfg      config.Config
}

type passwordRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	DeviceID   string `json:"deviceId,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
}

type authResponse struct {
	Data struct {
		User struct {
			ID            string   `json:"id"`
			Email         string   `json:"email"`
			EmailVerified bool     `json:"emailVerified"`
			Roles         []string `json:"roles"`
			AuthMethods   []string `json:"authMethods"`
		} `json:"user"`
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	} `json:"data"`
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	suite := contracttest.New(t)
	database := db.MustOpen(filepath.Join(t.TempDir(), "auth.db"))
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	mini := miniredis.RunT(t)
	rdb := redis.MustOpen("redis://" + mini.Addr() + "/0")
	t.Cleanup(func() { _ = rdb.Close() })
	cfg := testConfig(t)
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	server, err := newServer(cfg, logger, database, rdb)
	if err != nil {
		t.Fatalf("newServer() error = %v", err)
	}
	httpServer := httptest.NewServer(server.Handler)
	t.Cleanup(httpServer.Close)
	return &authFixture{suite: suite, client: testClient(), baseURL: httpServer.URL, database: database, cfg: cfg}
}

func (f *authFixture) post(t *testing.T, path string, body any) (*http.Request, contracttest.Response) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, contracttest.Do(t, f.client, req)
}

func (f *authFixture) register(t *testing.T, email string, deviceID string) authResponse {
	t.Helper()
	req, resp := f.post(t, "/v1/auth/password/register", passwordRequest{
		Email: email, Password: testPassword, DeviceID: deviceID, DeviceName: "iPhone 15",
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("register status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	return decodeAuthResponse(t, resp.Body)
}

func (f *authFixture) login(t *testing.T, email, password string, deviceID string) contracttest.Response {
	t.Helper()
	_, resp := f.post(t, "/v1/auth/password/login", passwordRequest{
		Email: email, Password: password, DeviceID: deviceID,
	})
	return resp
}

func decodeAuthResponse(t *testing.T, body []byte) authResponse {
	t.Helper()
	var decoded authResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode auth response: %v\nbody: %s", err, body)
	}
	return decoded
}

func assertAccessToken(t *testing.T, token, userID string) string {
	t.Helper()
	message, err := jws.Parse([]byte(token))
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	headers := message.Signatures()[0].ProtectedHeaders()
	if alg, ok := headers.Algorithm(); !ok || alg.String() != jwa.EdDSA().String() {
		t.Errorf("access token alg = %v, want EdDSA", alg)
	}
	if kid, ok := headers.KeyID(); !ok || kid != testKid {
		t.Errorf("access token kid = %q, want %q", kid, testKid)
	}
	var payload struct {
		Subject       string   `json:"sub"`
		SessionID     string   `json:"sid"`
		EmailVerified bool     `json:"email_verified"`
		Roles         []string `json:"roles"`
		Type          string   `json:"typ"`
		IssuedAt      int64    `json:"iat"`
		ExpiresAt     int64    `json:"exp"`
	}
	if err := json.Unmarshal(message.Payload(), &payload); err != nil {
		t.Fatalf("decode access token payload: %v", err)
	}
	if payload.Subject != userID {
		t.Errorf("access token sub = %q, want %q", payload.Subject, userID)
	}
	if payload.SessionID == "" {
		t.Error("access token sid is empty")
	}
	if payload.Type != "access" || payload.ExpiresAt-payload.IssuedAt != 900 {
		t.Errorf("access token typ/ttl = %q/%d, want access/900", payload.Type, payload.ExpiresAt-payload.IssuedAt)
	}
	if payload.EmailVerified {
		t.Error("access token email_verified = true, want false for a fresh registration")
	}
	if len(payload.Roles) != 1 || payload.Roles[0] != "USER" {
		t.Errorf("access token roles = %v, want [USER]", payload.Roles)
	}
	return payload.SessionID
}

func hashRefreshTokenValue(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestContractAuthRegisterIssuesSession(t *testing.T) {
	f := newAuthFixture(t)
	req, resp := f.post(t, "/v1/auth/password/register", passwordRequest{
		Email: "Dante@Example.com", Password: testPassword, DeviceID: "device-a", DeviceName: "iPhone 15",
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("register status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)

	decoded := decodeAuthResponse(t, resp.Body)
	user := decoded.Data.User
	if user.Email != "dante@example.com" {
		t.Errorf("email = %q, want normalized dante@example.com", user.Email)
	}
	if user.EmailVerified || len(user.Roles) != 1 || user.Roles[0] != "USER" {
		t.Errorf("user = %+v, want emailVerified false and roles [USER]", user)
	}
	if len(user.AuthMethods) != 1 || user.AuthMethods[0] != "PASSWORD" {
		t.Errorf("authMethods = %v, want [PASSWORD]", user.AuthMethods)
	}

	sessionID := assertAccessToken(t, decoded.Data.AccessToken, user.ID)
	if sessionID == "" {
		t.Fatal("no session id in access token")
	}

	var deviceID, activeKey string
	if err := f.database.SQL().QueryRowContext(t.Context(),
		"SELECT device_id, active_key FROM sessions WHERE id = ?", sessionID).Scan(&deviceID, &activeKey); err != nil {
		t.Fatalf("session row: %v", err)
	}
	if deviceID != "device-a" || activeKey != user.ID+":device-a" {
		t.Errorf("session device/activeKey = %q/%q, want device-a and the user:device key", deviceID, activeKey)
	}

	var storedHash string
	if err := f.database.SQL().QueryRowContext(t.Context(),
		"SELECT token_hash FROM refresh_tokens WHERE session_id = ?", sessionID).Scan(&storedHash); err != nil {
		t.Fatalf("refresh token row: %v", err)
	}
	if storedHash != hashRefreshTokenValue(decoded.Data.RefreshToken) {
		t.Error("stored refresh token hash does not match the issued token")
	}
}

func TestContractAuthLoginAndEmailNormalization(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "user@example.com", "")

	req, resp := f.post(t, "/v1/auth/password/login", passwordRequest{Email: "USER@example.com", Password: testPassword})
	if resp.Status != http.StatusOK {
		t.Fatalf("login status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	decoded := decodeAuthResponse(t, resp.Body)
	if decoded.Data.User.ID != registered.Data.User.ID {
		t.Errorf("login user id = %q, want %q", decoded.Data.User.ID, registered.Data.User.ID)
	}
	assertAccessToken(t, decoded.Data.AccessToken, registered.Data.User.ID)

	var count int
	if err := f.database.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sessions WHERE user_id = ?", registered.Data.User.ID).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 2 {
		t.Errorf("sessions = %d, want 2 (register + login)", count)
	}
}

func TestContractAuthRegisterErrors(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "taken@example.com", "")

	t.Run("duplicate email", func(t *testing.T) {
		_, resp := f.post(t, "/v1/auth/password/register", passwordRequest{Email: "taken@example.com", Password: testPassword})
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "AUTH_EMAIL_ALREADY_EXISTS")
		f.suite.AssertErrorCodeDeclared(t, "auth.password.register", "AUTH_EMAIL_ALREADY_EXISTS")
	})

	t.Run("short password", func(t *testing.T) {
		_, resp := f.post(t, "/v1/auth/password/register", passwordRequest{Email: "short@example.com", Password: "short"})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
		var problem struct {
			Title  string `json:"title"`
			Errors []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(resp.Body, &problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if problem.Title != "Validation Failed" {
			t.Errorf("title = %q, want Validation Failed", problem.Title)
		}
		if len(problem.Errors) != 1 || problem.Errors[0].Field != "password" {
			t.Errorf("errors = %+v, want one password issue", problem.Errors)
		}
	})

	t.Run("invalid email", func(t *testing.T) {
		_, resp := f.post(t, "/v1/auth/password/register", passwordRequest{Email: "not-an-email", Password: testPassword})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
	})
}

func TestContractAuthLoginInvalidCredentials(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "user2@example.com", "")

	for name, attempt := range map[string]passwordRequest{
		"wrong password": {Email: "user2@example.com", Password: "wrong-password"},
		"unknown email":  {Email: "nobody@example.com", Password: testPassword},
	} {
		t.Run(name, func(t *testing.T) {
			_, resp := f.post(t, "/v1/auth/password/login", attempt)
			if resp.Status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, "AUTH_INVALID_CREDENTIALS")
			f.suite.AssertErrorCodeDeclared(t, "auth.password.login", "AUTH_INVALID_CREDENTIALS")
		})
	}
}

func TestContractAuthSuspensionAndDeletion(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "moderated@example.com", "")

	update := func(status string) {
		t.Helper()
		if _, err := f.database.SQL().ExecContext(t.Context(),
			"UPDATE users SET status = ? WHERE email = ?", status, "moderated@example.com"); err != nil {
			t.Fatalf("update status: %v", err)
		}
	}

	update("SUSPENDED")
	_, resp := f.post(t, "/v1/auth/password/login", passwordRequest{Email: "moderated@example.com", Password: testPassword})
	if resp.Status != http.StatusForbidden {
		t.Fatalf("suspended login status = %d, want 403\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_USER_SUSPENDED")
	f.suite.AssertErrorCodeDeclared(t, "auth.password.login", "AUTH_USER_SUSPENDED")

	update("DELETED")
	_, resp = f.post(t, "/v1/auth/password/login", passwordRequest{Email: "moderated@example.com", Password: testPassword})
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("deleted login status = %d, want 401 (masked)\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_INVALID_CREDENTIALS")
}

func TestContractAuthLoginRateLimit(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "limited@example.com", "")

	for i := 0; i < 10; i++ {
		resp := f.login(t, "limited@example.com", "wrong-password", "")
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401\nbody: %s", i+1, resp.Status, resp.Body)
		}
	}

	resp := f.login(t, "limited@example.com", testPassword, "")
	if resp.Status != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt status = %d, want 429 (even with the correct password)\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "RATE_LIMITED")
	f.suite.AssertErrorCodeDeclared(t, "auth.password.login", "RATE_LIMITED")
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" || retryAfter == "0" {
		t.Fatalf("Retry-After = %q, want a positive number of seconds", retryAfter)
	}
}

func TestContractAuthLoginSuccessClearsRateLimit(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "reset@example.com", "")

	for i := 0; i < 5; i++ {
		if resp := f.login(t, "reset@example.com", "wrong-password", ""); resp.Status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, resp.Status)
		}
	}
	if resp := f.login(t, "reset@example.com", testPassword, ""); resp.Status != http.StatusOK {
		t.Fatalf("successful login status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	for i := 0; i < 6; i++ {
		if resp := f.login(t, "reset@example.com", "wrong-password", ""); resp.Status != http.StatusUnauthorized {
			t.Fatalf("post-reset attempt %d status = %d, want 401 (counters must have been cleared)", i+1, resp.Status)
		}
	}
	if resp := f.login(t, "reset@example.com", testPassword, ""); resp.Status != http.StatusOK {
		t.Fatalf("second successful login status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
}

func TestContractAuthSameDeviceRevokesPreviousSession(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "device@example.com", "device-a")
	userID := registered.Data.User.ID

	resp := f.login(t, "device@example.com", testPassword, "device-a")
	if resp.Status != http.StatusOK {
		t.Fatalf("login status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}

	var (
		total    int
		revoked  int
		active   int
		keyCount int
	)
	row := f.database.SQL().QueryRowContext(t.Context(), `
		SELECT
			COUNT(*),
			SUM(CASE WHEN revoked_at IS NOT NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN revoked_at IS NULL THEN 1 ELSE 0 END),
			SUM(CASE WHEN active_key IS NOT NULL THEN 1 ELSE 0 END)
		FROM sessions WHERE user_id = ?`, userID)
	if err := row.Scan(&total, &revoked, &active, &keyCount); err != nil {
		t.Fatalf("session aggregate: %v", err)
	}
	if total != 2 || revoked != 1 || active != 1 || keyCount != 1 {
		t.Errorf("sessions total/revoked/active/activeKey = %d/%d/%d/%d, want 2/1/1/1", total, revoked, active, keyCount)
	}

	var revokedTokens int
	if err := f.database.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM refresh_tokens WHERE revoked_at IS NOT NULL").Scan(&revokedTokens); err != nil {
		t.Fatalf("revoked refresh tokens: %v", err)
	}
	if revokedTokens != 1 {
		t.Errorf("revoked refresh tokens = %d, want 1 (the replaced session's)", revokedTokens)
	}
}
