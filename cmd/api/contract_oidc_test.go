//go:build !pg

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
)

const googleTestKid = "google-test-key"

type oidcFixture struct {
	*authFixture
	sign func(claims map[string]any) string
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate google key: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	jwks, err := json.Marshal(map[string]any{
		"keys": []any{map[string]any{
			"kty": "OKP",
			"crv": "Ed25519",
			"kid": googleTestKid,
			"alg": "EdDSA",
			"use": "sig",
			"x":   base64.RawURLEncoding.EncodeToString(publicKey),
		}},
	})
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwks)
	}))
	t.Cleanup(jwksServer.Close)

	fixture := newAuthFixtureWith(t, func(cfg *config.Config) {
		cfg.AuthOIDCGoogleClientIDs = "test-client-id"
		cfg.AuthOIDCGoogleJWKSURL = jwksServer.URL
	})
	return &oidcFixture{
		authFixture: fixture,
		sign: func(claims map[string]any) string {
			header := map[string]any{"alg": "EdDSA", "kid": googleTestKid, "typ": "JWT"}
			payload := map[string]any{
				"iss":            "https://accounts.google.com",
				"aud":            "test-client-id",
				"sub":            "google-subject-1",
				"email":          "dante@example.com",
				"email_verified": true,
				"name":           "Dante Alighieri",
				"given_name":     "Dante",
				"family_name":    "Alighieri",
				"iat":            time.Now().Add(-time.Minute).Unix(),
				"exp":            time.Now().Add(time.Hour).Unix(),
			}
			for key, value := range claims {
				payload[key] = value
			}
			headerJSON, _ := json.Marshal(header)
			payloadJSON, _ := json.Marshal(payload)
			signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
			signature := ed25519.Sign(privateKey, []byte(signingInput))
			return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
		},
	}
}

func (f *oidcFixture) exchange(t *testing.T, provider, idToken string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.post(t, "/v1/auth/oidc/exchange", map[string]any{
		"provider": provider, "idToken": idToken, "deviceId": "device-google",
	})
}

func TestContractAuthOidcExchangeCreatesUser(t *testing.T) {
	f := newOIDCFixture(t)
	req, resp := f.exchange(t, "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)

	decoded := decodeAuthResponse(t, resp.Body)
	user := decoded.Data.User
	if user.Email != "dante@example.com" || !user.EmailVerified {
		t.Errorf("user = %+v, want the verified google email", user)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "USER" {
		t.Errorf("roles = %v, want [USER]", user.Roles)
	}
	if len(user.AuthMethods) != 1 || user.AuthMethods[0] != "GOOGLE" {
		t.Errorf("authMethods = %v, want [GOOGLE]", user.AuthMethods)
	}
	assertAccessToken(t, decoded.Data.AccessToken, user.ID, true)

	var raw map[string]any
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		t.Fatalf("decode raw body: %v", err)
	}
	rawUser, _ := raw["data"].(map[string]any)["user"].(map[string]any)
	profile, _ := rawUser["profile"].(map[string]any)
	if profile["displayName"] != "Dante Alighieri" || profile["givenName"] != "Dante" || profile["familyName"] != "Alighieri" {
		t.Errorf("profile = %v, want the token names", profile)
	}

	var verifiedAt *string
	if err := f.database.SQL().QueryRowContext(t.Context(),
		"SELECT email_verified_at FROM users WHERE id = ?", user.ID).Scan(&verifiedAt); err != nil {
		t.Fatalf("user row: %v", err)
	}
	if verifiedAt == nil {
		t.Error("user email not marked verified")
	}
	var subject string
	if err := f.database.SQL().QueryRowContext(t.Context(),
		"SELECT subject FROM external_identities WHERE user_id = ? AND provider = 'GOOGLE'", user.ID).Scan(&subject); err != nil {
		t.Fatalf("identity row: %v", err)
	}
	if subject != "google-subject-1" {
		t.Errorf("subject = %q, want the token subject", subject)
	}
}

func TestContractAuthOidcExchangeReusesIdentity(t *testing.T) {
	f := newOIDCFixture(t)
	first := decodeAuthResponse(t, mustExchangeOK(t, f, f.sign(nil)).Body)

	second := decodeAuthResponse(t, mustExchangeOK(t, f, f.sign(nil)).Body)
	if second.Data.User.ID != first.Data.User.ID {
		t.Fatalf("second user = %q, want the existing %q", second.Data.User.ID, first.Data.User.ID)
	}

	var users, identities, sessions int
	if err := f.database.SQL().QueryRowContext(t.Context(), `
		SELECT
			(SELECT COUNT(*) FROM users),
			(SELECT COUNT(*) FROM external_identities),
			(SELECT COUNT(*) FROM sessions)`).Scan(&users, &identities, &sessions); err != nil {
		t.Fatalf("counts: %v", err)
	}
	if users != 1 || identities != 1 || sessions != 2 {
		t.Errorf("users/identities/sessions = %d/%d/%d, want 1/1/2", users, identities, sessions)
	}
}

func TestContractAuthOidcExchangeLinkRequired(t *testing.T) {
	f := newOIDCFixture(t)
	f.register(t, "dante@example.com", "")

	_, resp := f.exchange(t, "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusConflict {
		t.Fatalf("exchange status = %d, want 409\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_OIDC_LINK_REQUIRED")
	f.suite.AssertErrorCodeDeclared(t, "auth.oidc.exchange", "AUTH_OIDC_LINK_REQUIRED")

	var identities int
	if err := f.database.SQL().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM external_identities").Scan(&identities); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identities != 0 {
		t.Errorf("identities = %d, want 0 (no auto-link)", identities)
	}
}

func TestContractAuthOidcExchangeErrors(t *testing.T) {
	f := newOIDCFixture(t)
	validToken := f.sign(nil)

	cases := []struct {
		name     string
		provider string
		token    string
		status   int
		code     string
	}{
		{name: "tampered token", provider: "GOOGLE", token: tamperToken(validToken), status: 401, code: "AUTH_OIDC_TOKEN_INVALID"},
		{name: "expired token", provider: "GOOGLE", token: f.sign(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}), status: 401, code: "AUTH_OIDC_TOKEN_INVALID"},
		{name: "wrong audience", provider: "GOOGLE", token: f.sign(map[string]any{"aud": "someone-else"}), status: 401, code: "AUTH_OIDC_TOKEN_INVALID"},
		{name: "email not verified", provider: "GOOGLE", token: f.sign(map[string]any{"email_verified": false}), status: 400, code: "AUTH_OIDC_EMAIL_NOT_VERIFIED"},
		{name: "unknown provider", provider: "APPLE", token: validToken, status: 400, code: "VALIDATION_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp := f.exchange(t, tc.provider, tc.token)
			if resp.Status != tc.status {
				t.Fatalf("status = %d, want %d\nbody: %s", resp.Status, tc.status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, tc.code)
			f.suite.AssertErrorCodeDeclared(t, "auth.oidc.exchange", tc.code)
		})
	}
}

func TestContractAuthOidcExchangeSuspended(t *testing.T) {
	f := newOIDCFixture(t)
	decoded := decodeAuthResponse(t, mustExchangeOK(t, f, f.sign(nil)).Body)
	if _, err := f.database.SQL().ExecContext(t.Context(),
		"UPDATE users SET status = 'SUSPENDED' WHERE id = ?", decoded.Data.User.ID); err != nil {
		t.Fatalf("suspend user: %v", err)
	}
	_, resp := f.exchange(t, "GOOGLE", f.sign(nil))
	if resp.Status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_USER_SUSPENDED")
}

func TestContractAuthOidcNotConfigured(t *testing.T) {
	f := newAuthFixture(t)
	_, resp := f.post(t, "/v1/auth/oidc/exchange", map[string]any{"provider": "GOOGLE", "idToken": "whatever"})
	if resp.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_OIDC_NOT_CONFIGURED")
	f.suite.AssertErrorCodeDeclared(t, "auth.oidc.exchange", "AUTH_OIDC_NOT_CONFIGURED")
}

func mustExchangeOK(t *testing.T, f *oidcFixture, token string) contracttest.Response {
	t.Helper()
	_, resp := f.exchange(t, "GOOGLE", token)
	if resp.Status != http.StatusOK {
		t.Fatalf("exchange status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	return resp
}
