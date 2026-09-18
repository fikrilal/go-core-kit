package auth_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"

	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

var fixedMoment = time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)

func testKeysJSON(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	key, err := jwk.Import(privateKey)
	if err != nil {
		t.Fatalf("import key: %v", err)
	}
	if err := key.Set(jwk.KeyIDKey, "test-kid"); err != nil {
		t.Fatalf("set kid: %v", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(key); err != nil {
		t.Fatalf("add key: %v", err)
	}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}
	return string(raw)
}

func TestSignAccessTokenClaimsAndHeaders(t *testing.T) {
	keysJSON := testKeysJSON(t)
	ring, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{KeysJSON: keysJSON, ProductionLike: true})
	if err != nil {
		t.Fatalf("NewKeyRing() error = %v", err)
	}
	issuer := platformauth.NewIssuer(ring, clock.Fixed{Moment: fixedMoment}, "https://issuer.test", "api", 15*time.Minute)

	token, err := issuer.SignAccessToken(platformauth.AccessClaims{
		UserID:        "user-1",
		SessionID:     "session-1",
		EmailVerified: true,
		Roles:         []string{"USER"},
	})
	if err != nil {
		t.Fatalf("SignAccessToken() error = %v", err)
	}

	message, err := jws.Parse([]byte(token))
	if err != nil {
		t.Fatalf("jws.Parse() error = %v", err)
	}
	assertHeader(t, message)
	assertPayload(t, message)

	key, err := ring.SigningKey().PublicKey()
	if err != nil {
		t.Fatalf("SigningKey().PublicKey() error = %v", err)
	}
	if _, err := jws.Verify([]byte(token), jws.WithKey(jwa.EdDSA(), key)); err != nil {
		t.Fatalf("jws.Verify() error = %v", err)
	}
}

func assertHeader(t *testing.T, message *jws.Message) {
	t.Helper()
	headers := message.Signatures()[0].ProtectedHeaders()
	if alg, ok := headers.Algorithm(); !ok || alg.String() != jwa.EdDSA().String() {
		t.Errorf("alg = %v, want EdDSA", alg)
	}
	if kid, ok := headers.KeyID(); !ok || kid != "test-kid" {
		t.Errorf("kid = %q, want test-kid", kid)
	}
	if typ, ok := headers.Type(); !ok || typ != "JWT" {
		t.Errorf("typ = %q, want JWT", typ)
	}
}

func assertPayload(t *testing.T, message *jws.Message) {
	t.Helper()
	var payload struct {
		Subject       string   `json:"sub"`
		SessionID     string   `json:"sid"`
		EmailVerified bool     `json:"email_verified"`
		Roles         []string `json:"roles"`
		Type          string   `json:"typ"`
		IssuedAt      int64    `json:"iat"`
		ExpiresAt     int64    `json:"exp"`
		JTI           string   `json:"jti"`
		Issuer        string   `json:"iss"`
		Audience      string   `json:"aud"`
	}
	if err := json.Unmarshal(message.Payload(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Subject != "user-1" || payload.SessionID != "session-1" || !payload.EmailVerified || payload.Type != "access" {
		t.Errorf("payload = %+v, want the reference claim shape", payload)
	}
	if len(payload.Roles) != 1 || payload.Roles[0] != "USER" {
		t.Errorf("roles = %v, want [USER]", payload.Roles)
	}
	if payload.IssuedAt != fixedMoment.Unix() || payload.ExpiresAt != fixedMoment.Add(15*time.Minute).Unix() {
		t.Errorf("iat/exp = %d/%d, want %d/%d", payload.IssuedAt, payload.ExpiresAt, fixedMoment.Unix(), fixedMoment.Add(15*time.Minute).Unix())
	}
	if payload.JTI == "" {
		t.Error("jti is empty")
	}
	if payload.Issuer != "https://issuer.test" || payload.Audience != "api" {
		t.Errorf("iss/aud = %q/%q, want the configured values", payload.Issuer, payload.Audience)
	}
}

func TestSignAccessTokenOmitsEmptyIssuerAndAudience(t *testing.T) {
	keysJSON := testKeysJSON(t)
	ring, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{KeysJSON: keysJSON, ProductionLike: true})
	if err != nil {
		t.Fatalf("NewKeyRing() error = %v", err)
	}
	token, err := platformauth.NewIssuer(ring, clock.Fixed{Moment: fixedMoment}, "", "", time.Minute).SignAccessToken(platformauth.AccessClaims{UserID: "u"})
	if err != nil {
		t.Fatalf("SignAccessToken() error = %v", err)
	}
	var payload map[string]any
	parts := 0
	for _, part := range []byte(token) {
		if part == '.' {
			parts++
		}
	}
	if parts != 2 {
		t.Fatalf("token has %d dots, want a compact JWS", parts)
	}
	message, err := jws.Parse([]byte(token))
	if err != nil {
		t.Fatalf("jws.Parse() error = %v", err)
	}
	if err := json.Unmarshal(message.Payload(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if _, ok := payload["iss"]; ok {
		t.Error("iss present, want omitted")
	}
	if _, ok := payload["aud"]; ok {
		t.Error("aud present, want omitted")
	}
}

func TestKeyRingRequiresKeysInProductionLike(t *testing.T) {
	if _, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{ProductionLike: true}); err == nil {
		t.Fatal("NewKeyRing(production-like, no keys) = nil error, want error")
	}
}

func TestKeyRingEphemeralOutsideProduction(t *testing.T) {
	ring, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{})
	if err != nil {
		t.Fatalf("NewKeyRing(dev) error = %v", err)
	}
	if ring.Kid() == "" {
		t.Error("ephemeral ring has no kid")
	}
	if _, err := platformauth.NewIssuer(ring, clock.Fixed{Moment: fixedMoment}, "", "", time.Minute).SignAccessToken(platformauth.AccessClaims{UserID: "u"}); err != nil {
		t.Fatalf("SignAccessToken(ephemeral) error = %v", err)
	}
}

func TestKeyRingAcceptsBase64(t *testing.T) {
	keysJSON := testKeysJSON(t)
	encoded := base64.StdEncoding.EncodeToString([]byte(keysJSON))
	ring, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{KeysJSONBase64: encoded, ProductionLike: true})
	if err != nil {
		t.Fatalf("NewKeyRing(base64) error = %v", err)
	}
	if ring.Kid() != "test-kid" {
		t.Errorf("kid = %q, want test-kid", ring.Kid())
	}
}
