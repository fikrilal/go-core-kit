//go:build !pg

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
)

// testConfig is not Load. An empty issuer is valid for EnvTest.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		AppEnv:                     config.EnvTest,
		AuthAccessTokenTTLSeconds:  900,
		AuthRefreshTokenTTLSeconds: 2592000,
		AuthPasswordMinLength:      10,
		AuthLoginMaxAttempts:       10,
		AuthLoginWindowSeconds:     60,
		AuthLoginBlockSeconds:      900,
		AuthSigningKeysJSON:        testSigningKeys(t),
		// Profile image upload limits use the production defaults; tests that
		// need a different threshold mutate the fixture config.
		UsersProfileImageUploadUserMaxAttempts:   20,
		UsersProfileImageUploadUserWindowSeconds: 3600,
		UsersProfileImageUploadUserBlockSeconds:  900,
		UsersProfileImageUploadIPMaxAttempts:     60,
		UsersProfileImageUploadIPWindowSeconds:   300,
		UsersProfileImageUploadIPBlockSeconds:    900,
	}
}

func testSigningKeys(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	key, err := jwk.Import(privateKey)
	if err != nil {
		t.Fatalf("import signing key: %v", err)
	}
	if err := key.Set(jwk.KeyIDKey, "test-kid"); err != nil {
		t.Fatalf("set kid: %v", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(key); err != nil {
		t.Fatalf("add signing key: %v", err)
	}
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal signing keys: %v", err)
	}
	return string(raw)
}
