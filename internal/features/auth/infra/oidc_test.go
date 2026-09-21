package infra_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json" //nolint:depguard // the test fixture constructs JWKS and ID-token documents directly
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/features/auth/infra"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

var oidcMoment = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

const testKeyID = "google-test-key"

type googleFixture struct {
	verifier *infra.GoogleIDTokenVerifier
	private  ed25519.PrivateKey
	requests *atomic.Int64
}

func newGoogleFixture(t *testing.T, clientIDs string) *googleFixture {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	jwks := map[string]any{
		"keys": []any{map[string]any{
			"kty": "OKP",
			"crv": "Ed25519",
			"kid": testKeyID,
			"alg": "EdDSA",
			"use": "sig",
			"x":   base64.RawURLEncoding.EncodeToString(publicKey),
		}},
	}
	encoded, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshal jwks: %v", err)
	}
	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	t.Cleanup(server.Close)
	return &googleFixture{
		verifier: infra.NewGoogleIDTokenVerifier(clientIDs, server.URL, clock.Fixed{Moment: oidcMoment}),
		private:  privateKey,
		requests: requests,
	}
}

func (f *googleFixture) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "EdDSA", "kid": testKeyID, "typ": "JWT"}
	payload := map[string]any{
		"iss":            "https://accounts.google.com",
		"aud":            "test-client-id",
		"sub":            "google-subject-1",
		"email":          "dante@example.com",
		"email_verified": true,
		"exp":            oidcMoment.Add(time.Hour).Unix(),
		"iat":            oidcMoment.Add(-time.Minute).Unix(),
	}
	for key, value := range claims {
		payload[key] = value
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := ed25519.Sign(f.private, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func (f *googleFixture) verify(t *testing.T, token string) domain.OidcVerification {
	t.Helper()
	verification, err := f.verifier.VerifyIDToken(context.Background(), domain.AuthMethodGoogle, token)
	if err != nil {
		t.Fatalf("VerifyIDToken() error = %v", err)
	}
	return verification
}

func TestGoogleVerifierAcceptsValidToken(t *testing.T) {
	f := newGoogleFixture(t, "test-client-id")
	token := f.sign(t, map[string]any{"name": "Dante", "given_name": "Dante", "family_name": "Alighieri"})
	verification := f.verify(t, token)
	if verification.Kind != domain.OidcVerifyVerified {
		t.Fatalf("kind = %q, want verified", verification.Kind)
	}
	identity := verification.Identity
	if identity.Provider != domain.AuthMethodGoogle || identity.Subject != "google-subject-1" || identity.Email != "dante@example.com" || !identity.EmailVerified {
		t.Errorf("identity = %+v, want the token claims", identity)
	}
	if identity.DisplayName != "Dante" || identity.FamilyName != "Alighieri" {
		t.Errorf("profile names = %q/%q, want the token names", identity.DisplayName, identity.FamilyName)
	}
	if f.requests.Load() != 1 {
		t.Errorf("jwks requests = %d, want 1", f.requests.Load())
	}
	if _, err := f.verifier.VerifyIDToken(context.Background(), domain.AuthMethodGoogle, token); err != nil {
		t.Fatalf("second VerifyIDToken() error = %v", err)
	}
	if f.requests.Load() != 1 {
		t.Errorf("jwks requests = %d after a cached verification, want 1", f.requests.Load())
	}
}

func TestGoogleVerifierEmailVerifiedForms(t *testing.T) {
	f := newGoogleFixture(t, "test-client-id")
	verification := f.verify(t, f.sign(t, map[string]any{"email_verified": "true"}))
	if !verification.Identity.EmailVerified {
		t.Error("string email_verified \"true\" not accepted")
	}
	verification = f.verify(t, f.sign(t, map[string]any{"email_verified": "false"}))
	if verification.Identity.EmailVerified {
		t.Error("string email_verified \"false\" accepted as true")
	}
}

func TestGoogleVerifierRejections(t *testing.T) {
	f := newGoogleFixture(t, "test-client-id")
	validToken := f.sign(t, nil)

	cases := map[string]string{
		"garbage":          "not-a-token",
		"tampered":         validToken[:len(validToken)-2] + "AA",
		"wrong audience":   f.sign(t, map[string]any{"aud": "someone-else"}),
		"wrong issuer":     f.sign(t, map[string]any{"iss": "https://evil.example"}),
		"expired":          f.sign(t, map[string]any{"exp": oidcMoment.Add(-time.Minute).Unix()}),
		"missing subject":  f.sign(t, map[string]any{"sub": nil}),
		"missing email":    f.sign(t, map[string]any{"email": nil}),
		"bad verified":     f.sign(t, map[string]any{"email_verified": "maybe"}),
		"missing verified": f.sign(t, map[string]any{"email_verified": nil}),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if verification := f.verify(t, token); verification.Kind != domain.OidcVerifyInvalid {
				t.Fatalf("kind = %q, want invalid", verification.Kind)
			}
		})
	}
}

func TestGoogleVerifierNotConfiguredAndUnknownProvider(t *testing.T) {
	f := newGoogleFixture(t, "")
	if verification := f.verify(t, f.sign(t, nil)); verification.Kind != domain.OidcVerifyNotConfigured {
		t.Fatalf("kind = %q, want not_configured with empty client ids", verification.Kind)
	}
	configured := newGoogleFixture(t, "test-client-id")
	verification, err := configured.verifier.VerifyIDToken(context.Background(), "APPLE", configured.sign(t, nil))
	if err != nil {
		t.Fatalf("VerifyIDToken() error = %v", err)
	}
	if verification.Kind != domain.OidcVerifyInvalid {
		t.Fatalf("kind = %q, want invalid for an unknown provider", verification.Kind)
	}
	if configured.requests.Load() != 0 {
		t.Error("jwks fetched for an unknown provider")
	}
}

func TestGoogleVerifierFailsClosedWhenJWKSUnavailable(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	verifier := infra.NewGoogleIDTokenVerifier("test-client-id", server.URL, clock.Fixed{Moment: oidcMoment})

	fixture := &googleFixture{verifier: verifier, private: privateKey, requests: &atomic.Int64{}}
	if verification := fixture.verify(t, fixture.sign(t, nil)); verification.Kind != domain.OidcVerifyInvalid {
		t.Fatalf("kind = %q, want invalid when the JWKS is unavailable", verification.Kind)
	}
}
