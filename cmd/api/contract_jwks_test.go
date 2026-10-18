package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

func tamperToken(token string) string {
	index := strings.LastIndex(token, ".") + 1
	flipped := byte('A')
	if index < len(token) && token[index] == 'A' {
		flipped = 'B'
	}
	return token[:index] + string(flipped) + token[index+1:]
}

func TestContractJWKS(t *testing.T) {
	f := newAuthFixture(t)
	req := mustGet(t, f.baseURL+"/.well-known/jwks.json")
	resp := contracttest.Do(t, f.client, req)
	if resp.Status != http.StatusOK {
		t.Fatalf("jwks status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)

	var document struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(resp.Body, &document); err != nil {
		t.Fatalf("decode jwks: %v\nbody: %s", err, resp.Body)
	}
	if len(document.Keys) != 1 {
		t.Fatalf("jwks keys = %d, want the single signing key", len(document.Keys))
	}
	key := document.Keys[0]
	for field, want := range map[string]any{
		"kid": testKid,
		"kty": "OKP",
		"crv": "Ed25519",
		"use": "sig",
		"alg": "EdDSA",
	} {
		if key[field] != want {
			t.Errorf("jwks key %s = %v, want %v", field, key[field], want)
		}
	}
	if _, present := key["d"]; present {
		t.Error("jwks leaks private key material")
	}
}

func TestContractAccessTokenVerifiesEndToEnd(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "verify@example.com", "device-a")

	ring, err := platformauth.NewKeyRing(platformauth.KeyRingOptions{KeysJSON: f.cfg.AuthSigningKeysJSON, ProductionLike: true})
	if err != nil {
		t.Fatalf("NewKeyRing() error = %v", err)
	}
	verifier, err := platformauth.NewVerifier(ring, clock.System{}, "", "", false)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	principal, err := verifier.VerifyAccessToken(registered.Data.AccessToken)
	if err != nil {
		t.Fatalf("VerifyAccessToken() error = %v", err)
	}
	if principal.UserID != registered.Data.User.ID {
		t.Errorf("principal user = %q, want %q", principal.UserID, registered.Data.User.ID)
	}
	if principal.SessionID == "" || principal.EmailVerified {
		t.Errorf("principal = %+v, want a session id and unverified email", principal)
	}
	if len(principal.Roles) != 1 || principal.Roles[0] != "USER" {
		t.Errorf("roles = %v, want [USER]", principal.Roles)
	}

	var storedSessionID string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT id FROM sessions WHERE user_id = ?"), registered.Data.User.ID).Scan(&storedSessionID); err != nil {
		t.Fatalf("session row: %v", err)
	}
	if principal.SessionID != storedSessionID {
		t.Errorf("principal session = %q, want the stored session %q", principal.SessionID, storedSessionID)
	}

	if _, err := verifier.VerifyAccessToken(tamperToken(registered.Data.AccessToken)); !errors.Is(err, platformauth.ErrInvalidToken) {
		t.Errorf("tampered token error = %v, want ErrInvalidToken", err)
	}
}
