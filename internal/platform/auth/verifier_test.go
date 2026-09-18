package auth_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

func testKeyPair(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	key := map[string]any{
		"kty": "OKP",
		"crv": "Ed25519",
		"kid": "test-kid",
		"x":   base64.RawURLEncoding.EncodeToString(publicKey),
		"d":   base64.RawURLEncoding.EncodeToString(privateKey.Seed()),
	}
	set := map[string]any{"keys": []any{key}}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	return string(raw), privateKey
}

func testVerifierRing(t *testing.T, keysJSON string) *platformauth.KeyRing {
	t.Helper()
	ring, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{KeysJSON: keysJSON, ProductionLike: true})
	if err != nil {
		t.Fatalf("NewKeyRing() error = %v", err)
	}
	return ring
}

func signRaw(t *testing.T, privateKey ed25519.PrivateKey, header, payload map[string]any) string {
	t.Helper()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func baseHeader() map[string]any {
	return map[string]any{"alg": "EdDSA", "kid": "test-kid", "typ": "JWT"}
}

func baseClaims() map[string]any {
	return map[string]any{
		"sub":            "user-1",
		"sid":            "session-1",
		"jti":            "jti-1",
		"typ":            "access",
		"email_verified": true,
		"roles":          []any{"USER"},
		"iat":            fixedMoment.Unix(),
		"exp":            fixedMoment.Add(15 * time.Minute).Unix(),
	}
}

func tamperedToken(token string) string {
	index := strings.LastIndex(token, ".") + 1
	flipped := byte('A')
	if index < len(token) && token[index] == 'A' {
		flipped = 'B'
	}
	return token[:index] + string(flipped) + token[index+1:]
}

func TestVerifyAccessTokenRoundTrip(t *testing.T) {
	keysJSON, _ := testKeyPair(t)
	ring := testVerifierRing(t, keysJSON)
	issuer := platformauth.NewIssuer(ring, clock.Fixed{Moment: fixedMoment}, "", "", 15*time.Minute)
	verifier, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: fixedMoment}, "", "", false)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	token, err := issuer.SignAccessToken(platformauth.AccessClaims{
		UserID: "user-1", SessionID: "session-1", EmailVerified: true, Roles: []string{"USER"},
	})
	if err != nil {
		t.Fatalf("SignAccessToken() error = %v", err)
	}
	principal, err := verifier.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	if principal.UserID != "user-1" || principal.SessionID != "session-1" || !principal.EmailVerified {
		t.Errorf("principal = %+v, want the issued claims", principal)
	}
	if len(principal.Roles) != 1 || principal.Roles[0] != "USER" {
		t.Errorf("roles = %v, want [USER]", principal.Roles)
	}
}

func TestVerifyAccessTokenRejections(t *testing.T) {
	keysJSON, privateKey := testKeyPair(t)
	ring := testVerifierRing(t, keysJSON)
	otherJSON, otherKey := testKeyPair(t)
	otherRing := testVerifierRing(t, otherJSON)

	verifier := func(t *testing.T, issuer, audience string) *platformauth.Verifier {
		t.Helper()
		v, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: fixedMoment}, issuer, audience, false)
		if err != nil {
			t.Fatalf("NewVerifier() error = %v", err)
		}
		return v
	}

	validToken := signRaw(t, privateKey, baseHeader(), baseClaims())

	cases := []struct {
		name   string
		token  string
		verify *platformauth.Verifier
	}{
		{name: "empty", token: "", verify: verifier(t, "", "")},
		{name: "garbage", token: "not-a-token", verify: verifier(t, "", "")},
		{name: "oversized", token: strings.Repeat("a", 16_385), verify: verifier(t, "", "")},
		{name: "tampered signature", token: tamperedToken(validToken), verify: verifier(t, "", "")},
		{name: "unknown kid", token: signRaw(t, privateKey, map[string]any{"alg": "EdDSA", "kid": "other-kid", "typ": "JWT"}, baseClaims()), verify: verifier(t, "", "")},
		{name: "alg mismatch", token: signRaw(t, privateKey, map[string]any{"alg": "RS256", "kid": "test-kid", "typ": "JWT"}, baseClaims()), verify: verifier(t, "", "")},
		{name: "signature from another key", token: signRaw(t, otherKey, baseHeader(), baseClaims()), verify: verifier(t, "", "")},
		{name: "expired", token: validToken, verify: verifierAt(t, ring, fixedMoment.Add(16*time.Minute))},
		{name: "future iat", token: validToken, verify: verifierAt(t, ring, fixedMoment.Add(-2*time.Minute))},
		{name: "iat after exp", token: signRaw(t, privateKey, baseHeader(), withClaim(baseClaims(), "iat", fixedMoment.Add(20*time.Minute).Unix())), verify: verifier(t, "", "")},
		{name: "nbf in the future", token: signRaw(t, privateKey, baseHeader(), withClaim(baseClaims(), "nbf", fixedMoment.Add(2*time.Minute).Unix())), verify: verifier(t, "", "")},
		{name: "issuer mismatch", token: signRaw(t, privateKey, baseHeader(), withClaim(baseClaims(), "iss", "https://other.test")), verify: verifier(t, "https://issuer.test", "")},
		{name: "audience mismatch", token: signRaw(t, privateKey, baseHeader(), withClaim(baseClaims(), "aud", "other")), verify: verifier(t, "", "api")},
		{name: "wrong token type", token: signRaw(t, privateKey, baseHeader(), withClaim(baseClaims(), "typ", "refresh")), verify: verifier(t, "", "")},
		{name: "missing subject", token: signRaw(t, privateKey, baseHeader(), withoutClaim(baseClaims(), "sub")), verify: verifier(t, "", "")},
		{name: "missing session", token: signRaw(t, privateKey, baseHeader(), withoutClaim(baseClaims(), "sid")), verify: verifier(t, "", "")},
		{name: "missing jti", token: signRaw(t, privateKey, baseHeader(), withoutClaim(baseClaims(), "jti")), verify: verifier(t, "", "")},
		{name: "jti too long", token: signRaw(t, privateKey, baseHeader(), withClaim(baseClaims(), "jti", strings.Repeat("j", 129))), verify: verifier(t, "", "")},
		{name: "missing exp", token: signRaw(t, privateKey, baseHeader(), withoutClaim(baseClaims(), "exp")), verify: verifier(t, "", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.verify.VerifyAccessToken(tc.token); !errors.Is(err, platformauth.ErrInvalidToken) {
				t.Fatalf("VerifyAccessToken() error = %v, want ErrInvalidToken", err)
			}
		})
	}
	_ = otherRing
}

func verifierAt(t *testing.T, ring *platformauth.KeyRing, moment time.Time) *platformauth.Verifier {
	t.Helper()
	v, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: moment}, "", "", false)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	return v
}

func withClaim(claims map[string]any, name string, value any) map[string]any {
	claims[name] = value
	return claims
}

func withoutClaim(claims map[string]any, name string) map[string]any {
	delete(claims, name)
	return claims
}

func TestVerifyAccessTokenAcceptsArrayAudienceAndSanitizesRoles(t *testing.T) {
	keysJSON, privateKey := testKeyPair(t)
	ring := testVerifierRing(t, keysJSON)
	verifier, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: fixedMoment}, "", "api", false)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	claims := baseClaims()
	claims["aud"] = []any{"web", "api"}
	claims["email_verified"] = "yes"
	claims["roles"] = []any{1, " USER ", "USER", "", "ADMIN"}
	token := signRaw(t, privateKey, baseHeader(), claims)

	principal, err := verifier.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	if principal.EmailVerified {
		t.Error("non-boolean email_verified accepted as true")
	}
	if len(principal.Roles) != 2 || principal.Roles[0] != "USER" || principal.Roles[1] != "ADMIN" {
		t.Errorf("roles = %v, want [USER ADMIN]", principal.Roles)
	}
}

func TestNewVerifierRequiresIssuerAndAudienceInProductionLike(t *testing.T) {
	keysJSON, _ := testKeyPair(t)
	ring := testVerifierRing(t, keysJSON)
	if _, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: fixedMoment}, "", "", true); err == nil {
		t.Fatal("NewVerifier(production-like, no issuer/audience) = nil error, want error")
	}
	if _, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: fixedMoment}, "https://issuer.test", "api", true); err != nil {
		t.Fatalf("NewVerifier(production-like, configured) error = %v", err)
	}
}
