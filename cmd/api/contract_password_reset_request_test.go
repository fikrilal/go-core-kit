package main

import (
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
)

func TestContractAuthPasswordResetRequest(t *testing.T) {
	// Without a sender the provider gate answers, and the body is validated
	// before anything else.
	unconfigured := newAuthFixture(t)
	_, gated := unconfigured.post(t, "/v1/auth/password/reset/request", map[string]string{"email": "someone@example.com"})
	contracttest.AssertProblem(t, gated, "AUTH_EMAIL_NOT_CONFIGURED")

	_, invalid := unconfigured.post(t, "/v1/auth/password/reset/request", map[string]string{})
	contracttest.AssertProblem(t, invalid, "VALIDATION_FAILED")

	f := newAuthFixtureWithQueue(t, func(cfg *config.Config) {
		cfg.ResendAPIKey = "resend-key"
		cfg.EmailFrom = "auth@example.com"
		cfg.AuthPasswordResetRequestMaxAttempts = 1
		cfg.AuthPasswordResetRequestWindowSeconds = 60
		cfg.AuthPasswordResetRequestBlockSeconds = 60
	})

	// An unknown address answers exactly like a known one: 204, nothing sent.
	req, resp := f.post(t, "/v1/auth/password/reset/request", map[string]string{"email": "unknown@example.com"})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("request status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	assertGoldenShapePath(t, req, resp.Status, resp.Body)
	contracttest.AssertRequestID(t, resp)

	// The address is metered, so the next attempt is the declared limit problem.
	_, limited := f.post(t, "/v1/auth/password/reset/request", map[string]string{"email": "unknown@example.com"})
	contracttest.AssertProblem(t, limited, "RATE_LIMITED")
}
