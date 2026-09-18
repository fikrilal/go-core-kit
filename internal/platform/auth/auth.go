// Package auth provides JWT key management, access-token issuance, and verification.
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// JWKS is the published JSON Web Key Set document.
type JWKS struct {
	Keys []jwk.Key `json:"keys"`
}

// KeyRing holds the access-token signing key and its published public keys.
type KeyRing struct {
	signingKey jwk.Key
	kid        string
	publicKeys []jwk.Key
	keysByKid  map[string]jwk.Key
}

// KeyRingOptions configure key loading.
type KeyRingOptions struct {
	KeysJSON       string
	KeysJSONBase64 string
	ProductionLike bool
}

// NewKeyRing loads a signing key from a JWK set, falling back to an ephemeral
// key outside production-like environments. Staging and production require keys.
func NewKeyRing(opts KeyRingOptions) (*KeyRing, error) {
	raw := strings.TrimSpace(opts.KeysJSON)
	if raw == "" && strings.TrimSpace(opts.KeysJSONBase64) != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(opts.KeysJSONBase64))
		if err != nil {
			return nil, fmt.Errorf("auth: decode signing keys: %w", err)
		}
		raw = strings.TrimSpace(string(decoded))
	}
	if raw == "" {
		if opts.ProductionLike {
			return nil, fmt.Errorf("auth: AUTH_SIGNING_KEYS_JSON (or AUTH_SIGNING_KEYS_JSON_BASE64) is required when APP_ENV is staging or production")
		}
		return ephemeralKeyRing()
	}

	set, err := jwk.Parse([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("auth: parse signing keys: %w", err)
	}
	ring := &KeyRing{keysByKid: map[string]jwk.Key{}}
	for i := range set.Len() {
		if key, ok := set.Key(i); ok {
			ring.addKey(key)
		}
	}
	if ring.signingKey == nil {
		if opts.ProductionLike {
			return nil, fmt.Errorf("auth: AUTH_SIGNING_KEYS_JSON did not contain any usable private keys")
		}
		return ephemeralKeyRing()
	}
	kid, _ := ring.signingKey.KeyID()
	ring.kid = kid
	return ring, nil
}

func (k *KeyRing) addKey(key jwk.Key) {
	kid, ok := key.KeyID()
	if !ok || strings.TrimSpace(kid) == "" {
		return
	}
	isPrivate, err := jwk.IsPrivateKey(key)
	if err != nil {
		return
	}
	publicKey := key
	if isPrivate {
		derived, err := key.PublicKey()
		if err != nil {
			return
		}
		publicKey = derived
	}
	if err := publicKey.Set(jwk.KeyIDKey, kid); err != nil {
		return
	}
	if err := publicKey.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return
	}
	if err := publicKey.Set(jwk.AlgorithmKey, jwa.EdDSA()); err != nil {
		return
	}
	k.publicKeys = append(k.publicKeys, publicKey)
	k.keysByKid[kid] = publicKey
	if isPrivate && k.signingKey == nil {
		k.signingKey = key
	}
}

func ephemeralKeyRing() (*KeyRing, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("auth: generate ephemeral key: %w", err)
	}
	key, err := jwk.Import(privateKey)
	if err != nil {
		return nil, fmt.Errorf("auth: import ephemeral key: %w", err)
	}
	kid := uuid.NewString()
	if err := key.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, fmt.Errorf("auth: set ephemeral kid: %w", err)
	}
	publicKey, err := key.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("auth: derive ephemeral public key: %w", err)
	}
	if err := publicKey.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, fmt.Errorf("auth: publish ephemeral key: %w", err)
	}
	if err := publicKey.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return nil, fmt.Errorf("auth: publish ephemeral key: %w", err)
	}
	if err := publicKey.Set(jwk.AlgorithmKey, jwa.EdDSA()); err != nil {
		return nil, fmt.Errorf("auth: publish ephemeral key: %w", err)
	}
	return &KeyRing{
		signingKey: key,
		kid:        kid,
		publicKeys: []jwk.Key{publicKey},
		keysByKid:  map[string]jwk.Key{kid: publicKey},
	}, nil
}

// SigningKey returns the key used to sign access tokens.
func (k *KeyRing) SigningKey() jwk.Key {
	return k.signingKey
}

// Kid returns the key id published in token headers.
func (k *KeyRing) Kid() string {
	return k.kid
}

// PublicJWKS returns the publishable key set document.
func (k *KeyRing) PublicJWKS() JWKS {
	return JWKS{Keys: slices.Clone(k.publicKeys)}
}

// PublicKeyForKid returns the verification key for a key id.
func (k *KeyRing) PublicKeyForKid(kid string) (jwk.Key, bool) {
	key, ok := k.keysByKid[kid]
	return key, ok
}

// Issuer signs first-party access tokens with EdDSA.
type Issuer struct {
	ring     *KeyRing
	clock    clock.Clock
	issuer   string
	audience string
	ttl      time.Duration
}

// NewIssuer builds an access-token issuer.
func NewIssuer(ring *KeyRing, clk clock.Clock, issuer, audience string, ttl time.Duration) *Issuer {
	return &Issuer{ring: ring, clock: clk, issuer: issuer, audience: audience, ttl: ttl}
}

// AccessClaims are the client-visible claims of an access token.
type AccessClaims struct {
	UserID        string
	SessionID     string
	EmailVerified bool
	Roles         []string
}

type accessTokenPayload struct {
	Subject       string   `json:"sub"`
	SessionID     string   `json:"sid"`
	EmailVerified bool     `json:"email_verified"`
	Roles         []string `json:"roles"`
	Type          string   `json:"typ"`
	IssuedAt      int64    `json:"iat"`
	ExpiresAt     int64    `json:"exp"`
	JTI           string   `json:"jti"`
	Issuer        string   `json:"iss,omitempty"`
	Audience      string   `json:"aud,omitempty"`
}

// SignAccessToken signs the claims as an EdDSA JWT with the ring's key.
func (i *Issuer) SignAccessToken(claims AccessClaims) (string, error) {
	now := i.clock.Now()
	payload, err := json.Marshal(accessTokenPayload{
		Subject:       claims.UserID,
		SessionID:     claims.SessionID,
		EmailVerified: claims.EmailVerified,
		Roles:         claims.Roles,
		Type:          "access",
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(i.ttl).Unix(),
		JTI:           uuid.NewString(),
		Issuer:        i.issuer,
		Audience:      i.audience,
	})
	if err != nil {
		return "", fmt.Errorf("auth: encode access token: %w", err)
	}
	headers := jws.NewHeaders()
	if err := headers.Set(jws.TypeKey, "JWT"); err != nil {
		return "", fmt.Errorf("auth: set token type header: %w", err)
	}
	if err := headers.Set(jws.KeyIDKey, i.ring.Kid()); err != nil {
		return "", fmt.Errorf("auth: set token kid header: %w", err)
	}
	signed, err := jws.Sign(payload, jws.WithKey(jwa.EdDSA(), i.ring.SigningKey(), jws.WithProtectedHeaders(headers)))
	if err != nil {
		return "", fmt.Errorf("auth: sign access token: %w", err)
	}
	return string(signed), nil
}
