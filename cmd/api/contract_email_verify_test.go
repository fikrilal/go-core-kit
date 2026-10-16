//go:build !pg

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
)

func TestContractAuthEmailVerify(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "verify@example.com", "device-1")

	// The worker mints tokens in production; the fixture writes one directly so
	// the endpoint can be driven without the queue. The hash mirrors
	// domain.HashEmailVerificationToken: SHA-256, base64url without padding.
	token := "verify-token-1"
	seedAuthToken(t, f, "email_verification_tokens", "token-1", "verify@example.com", token, time.Now().Add(time.Hour))

	req, resp := f.post(t, "/v1/auth/email/verify", map[string]string{"token": token})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("verify status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)

	// A replayed token is expired, not invalid: the kit distinguishes the two.
	_, replayed := f.post(t, "/v1/auth/email/verify", map[string]string{"token": token})
	assertAuthVerifyProblem(t, replayed, http.StatusUnauthorized, "AUTH_EMAIL_VERIFICATION_TOKEN_EXPIRED")

	_, unknown := f.post(t, "/v1/auth/email/verify", map[string]string{"token": "not-a-real-token"})
	assertAuthVerifyProblem(t, unknown, http.StatusUnauthorized, "AUTH_EMAIL_VERIFICATION_TOKEN_INVALID")

	_, missing := f.post(t, "/v1/auth/email/verify", map[string]string{})
	assertAuthVerifyProblem(t, missing, http.StatusBadRequest, "VALIDATION_FAILED")
}

// seedAuthToken writes a live token row for the account, hashing the token the
// way domain.HashEmailVerificationToken and domain.HashPasswordResetToken do.
func seedAuthToken(t *testing.T, f *authFixture, table, id, email, token string, expiresAt time.Time) {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	hash := base64.RawURLEncoding.EncodeToString(sum[:])
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	query := "INSERT INTO " + table + " (id, user_id, token_hash, expires_at, created_at, updated_at) VALUES (?, (SELECT id FROM users WHERE email = ?), ?, ?, ?, ?)"
	if _, err := f.database.SQL().ExecContext(t.Context(), query,
		id, email, hash, expiresAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), stamp, stamp); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

// assertAuthVerifyProblem checks the full RFC7807 problem the endpoint answered.
func assertAuthVerifyProblem(t *testing.T, resp contracttest.Response, status int, code string) {
	t.Helper()
	if resp.Status != status {
		t.Fatalf("status = %d, want %d\nbody: %s", resp.Status, status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, code)
	contracttest.AssertRequestID(t, resp)
}
