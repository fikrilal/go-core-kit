package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
)

func TestContractAuthPasswordResetConfirm(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "reset@example.com", "device-1")

	// The worker mints reset tokens in production; the fixture writes one
	// directly. The hash mirrors domain.HashPasswordResetToken (SHA-256,
	// base64url unpadded).
	token := "reset-token-1"
	seedAuthToken(t, f, "password_reset_tokens", "reset-1", "reset@example.com", token, time.Now().Add(30*time.Minute))

	req, resp := f.post(t, "/v1/auth/password/reset/confirm", map[string]string{
		"token": token, "password": testPassword,
	})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("confirm status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)

	// A replayed token is expired, and the new password works.
	_, replayed := f.post(t, "/v1/auth/password/reset/confirm", map[string]string{
		"token": token, "password": testPassword,
	})
	contracttest.AssertProblem(t, replayed, "AUTH_PASSWORD_RESET_TOKEN_EXPIRED")
	if login := f.login(t, "reset@example.com", testPassword, "device-2"); login.Status != http.StatusOK {
		t.Errorf("login with the reset password = %d, want 200\nbody: %s", login.Status, login.Body)
	}

	_, unknown := f.post(t, "/v1/auth/password/reset/confirm", map[string]string{
		"token": "not-a-real-token", "password": testPassword,
	})
	contracttest.AssertProblem(t, unknown, "AUTH_PASSWORD_RESET_TOKEN_INVALID")

	_, missing := f.post(t, "/v1/auth/password/reset/confirm", map[string]string{"token": token})
	contracttest.AssertProblem(t, missing, "VALIDATION_FAILED")
}
