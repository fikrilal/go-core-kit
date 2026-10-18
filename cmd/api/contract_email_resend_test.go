package main

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
)

func TestContractAuthEmailVerificationResend(t *testing.T) {
	f := newAuthFixture(t)

	// Without a session the guard answers before the provider gate.
	_, anonymous := f.post(t, "/v1/auth/email/verification/resend", map[string]string{})
	contracttest.AssertProblem(t, anonymous, "UNAUTHORIZED")
	contracttest.AssertRequestID(t, anonymous)

	// With a session the gate answers: this fixture configures no email sender,
	// so the route reports the provider as unconfigured.
	session := f.register(t, "resend@example.com", "device-1")
	req, resp := postAuthed(t, f, "/v1/auth/email/verification/resend", session.Data.AccessToken)
	contracttest.AssertProblem(t, resp, "AUTH_EMAIL_NOT_CONFIGURED")
	contracttest.AssertRequestID(t, resp)
	if got := req.Header.Get("Authorization"); got == "" {
		t.Error("the authenticated request must carry the session token")
	}
}

// postAuthed sends a bodyless POST with the session's access token.
func postAuthed(t *testing.T, f *authFixture, path, accessToken string) (*http.Request, contracttest.Response) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.baseURL+path, bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return req, contracttest.Do(t, f.client, req)
}

// TestContractAuthEmailVerificationResendEnqueues covers the live success path:
// with a sender configured the guarded route answers 204, and the per-account
// limit turns the next attempt into the declared RATE_LIMITED problem.
func TestContractAuthEmailVerificationResendEnqueues(t *testing.T) {
	f := newAuthFixtureWithQueue(t, func(cfg *config.Config) {
		cfg.ResendAPIKey = "resend-key"
		cfg.EmailFrom = "auth@example.com"
		cfg.AuthEmailResendMaxAttempts = 1
		cfg.AuthEmailResendWindowSeconds = 60
		cfg.AuthEmailResendBlockSeconds = 60
	})
	session := f.register(t, "resend-live@example.com", "device-1")

	req, resp := postAuthed(t, f, "/v1/auth/email/verification/resend", session.Data.AccessToken)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("resend status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)

	_, limited := postAuthed(t, f, "/v1/auth/email/verification/resend", session.Data.AccessToken)
	if limited.Status != http.StatusTooManyRequests {
		t.Fatalf("second resend status = %d, want 429\nbody: %s", limited.Status, limited.Body)
	}
	contracttest.AssertProblem(t, limited, "RATE_LIMITED")
}
