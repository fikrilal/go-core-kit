package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

// jsonRequest posts a JSON body with optional bearer and idempotency headers.
func (f *authFixture) jsonRequest(t *testing.T, path, token, idempotencyKey string, body any) (*http.Request, contracttest.Response) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return req, contracttest.Do(t, f.client, req)
}

func (f *oidcFixture) connect(t *testing.T, token, idempotencyKey, provider, idToken string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.jsonRequest(t, "/v1/auth/oidc/connect", token, idempotencyKey,
		map[string]any{"provider": provider, "idToken": idToken})
}

func (f *oidcFixture) mustConnectOK(t *testing.T, token string) *http.Request {
	t.Helper()
	req, resp := f.connect(t, token, "", "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusNoContent {
		t.Fatalf("connect status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	return req
}

func TestContractAuthOidcConnectHappyPath(t *testing.T) {
	f := newOIDCFixture(t)
	registered := f.register(t, "dante@example.com", "")

	req, resp := f.connect(t, registered.Data.AccessToken, "connect-key-1", "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusNoContent {
		t.Fatalf("connect status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)
	if len(resp.Body) != 0 {
		t.Errorf("connect body = %q, want empty", resp.Body)
	}

	replayReq, replay := f.connect(t, registered.Data.AccessToken, "connect-key-1", "GOOGLE", f.sign(nil))
	if replay.Status != http.StatusNoContent {
		t.Fatalf("replay status = %d, want the cached 204\nbody: %s", replay.Status, replay.Body)
	}
	if replayed := replay.Header.Get("Idempotency-Replayed"); replayed != "true" {
		t.Errorf("replay header = %q, want Idempotency-Replayed: true", replayed)
	}
	_ = replayReq

	var verifiedAt *string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT email_verified_at FROM users WHERE id = ?"), registered.Data.User.ID).Scan(&verifiedAt); err != nil {
		t.Fatalf("user row: %v", err)
	}
	if verifiedAt == nil {
		t.Error("email not marked verified although the identity email matches")
	}
	var subject, email string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT subject, email FROM external_identities WHERE user_id = ? AND provider = 'GOOGLE'"), registered.Data.User.ID).Scan(&subject, &email); err != nil {
		t.Fatalf("identity row: %v", err)
	}
	if subject != "google-subject-1" || email != "dante@example.com" {
		t.Errorf("identity = %q/%q, want the token subject and email", subject, email)
	}
}

func TestContractAuthOidcConnectDoesNotVerifyOtherEmail(t *testing.T) {
	f := newOIDCFixture(t)
	registered := f.register(t, "other@example.com", "")
	f.mustConnectOK(t, registered.Data.AccessToken)

	var verifiedAt *string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT email_verified_at FROM users WHERE id = ?"), registered.Data.User.ID).Scan(&verifiedAt); err != nil {
		t.Fatalf("user row: %v", err)
	}
	if verifiedAt != nil {
		t.Errorf("email_verified_at = %q, want null: the identity email does not match", *verifiedAt)
	}
	var profiles int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM user_profiles")).Scan(&profiles); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if profiles != 0 {
		t.Errorf("profiles = %d, want 0: connect does not create a profile", profiles)
	}
}

func TestContractAuthOidcConnectRepeatIsIdempotent(t *testing.T) {
	f := newOIDCFixture(t)
	registered := f.register(t, "dante@example.com", "")
	f.mustConnectOK(t, registered.Data.AccessToken)
	f.mustConnectOK(t, registered.Data.AccessToken)

	var identities int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM external_identities")).Scan(&identities); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identities != 1 {
		t.Errorf("identities = %d, want 1 after a repeated connect", identities)
	}
}

func TestContractAuthOidcConnectConflicts(t *testing.T) {
	t.Run("identity owned by another user", func(t *testing.T) {
		f := newOIDCFixture(t)
		first := f.register(t, "dante@example.com", "")
		f.mustConnectOK(t, first.Data.AccessToken)

		second := f.register(t, "beatrice@example.com", "")
		_, resp := f.connect(t, second.Data.AccessToken, "", "GOOGLE", f.sign(nil))
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "AUTH_OIDC_IDENTITY_ALREADY_LINKED")
		f.suite.AssertErrorCodeDeclared(t, "auth.oidc.connect", "AUTH_OIDC_IDENTITY_ALREADY_LINKED")
	})

	t.Run("provider already linked", func(t *testing.T) {
		f := newOIDCFixture(t)
		first := f.register(t, "dante@example.com", "")
		f.mustConnectOK(t, first.Data.AccessToken)

		_, resp := f.connect(t, first.Data.AccessToken, "", "GOOGLE", f.sign(map[string]any{"sub": "google-subject-2"}))
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "AUTH_OIDC_PROVIDER_ALREADY_LINKED")
		f.suite.AssertErrorCodeDeclared(t, "auth.oidc.connect", "AUTH_OIDC_PROVIDER_ALREADY_LINKED")
	})
}

func TestContractAuthOidcConnectErrors(t *testing.T) {
	f := newOIDCFixture(t)
	registered := f.register(t, "dante@example.com", "")
	token := registered.Data.AccessToken
	validToken := f.sign(nil)

	cases := []struct {
		name     string
		token    string
		provider string
		idToken  string
		status   int
		code     string
	}{
		{name: "missing token", token: "", provider: "GOOGLE", idToken: validToken, status: 401, code: "UNAUTHORIZED"},
		{name: "tampered token", token: token, provider: "GOOGLE", idToken: tamperToken(validToken), status: 401, code: "AUTH_OIDC_TOKEN_INVALID"},
		{name: "email not verified", token: token, provider: "GOOGLE", idToken: f.sign(map[string]any{"email_verified": false}), status: 400, code: "AUTH_OIDC_EMAIL_NOT_VERIFIED"},
		{name: "unknown provider", token: token, provider: "APPLE", idToken: validToken, status: 400, code: "VALIDATION_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp := f.connect(t, tc.token, "", tc.provider, tc.idToken)
			if resp.Status != tc.status {
				t.Fatalf("status = %d, want %d\nbody: %s", resp.Status, tc.status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, tc.code)
			f.suite.AssertErrorCodeDeclared(t, "auth.oidc.connect", tc.code)
		})
	}
}

func TestContractAuthOidcConnectDeletedUserIsUnauthorized(t *testing.T) {
	f := newOIDCFixture(t)
	registered := f.register(t, "dante@example.com", "")
	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = 'DELETED' WHERE id = ?"), registered.Data.User.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	_, resp := f.connect(t, registered.Data.AccessToken, "", "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
}

func TestContractAuthOidcConnectNotConfigured(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "dante@example.com", "")
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.baseURL+"/v1/auth/oidc/connect",
		bytes.NewReader([]byte(`{"provider":"GOOGLE","idToken":"whatever"}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+registered.Data.AccessToken)
	resp := contracttest.Do(t, f.client, req)
	if resp.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_OIDC_NOT_CONFIGURED")
	f.suite.AssertErrorCodeDeclared(t, "auth.oidc.connect", "AUTH_OIDC_NOT_CONFIGURED")
}

func TestContractAuthOidcConnectGuardPrecedesIdempotency(t *testing.T) {
	f := newOIDCFixture(t)
	registered := f.register(t, "dante@example.com", "")
	token := registered.Data.AccessToken

	_, resp := f.connect(t, "", "connect-outer-key", "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", resp.Status)
	}

	_, resp = f.connect(t, token, "connect-outer-key", "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusNoContent {
		t.Fatalf("authorized status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	if replayed := resp.Header.Get("Idempotency-Replayed"); replayed != "" {
		t.Errorf("Idempotency-Replayed = %q, want empty: the unauthorized request must not prime the store", replayed)
	}
}
