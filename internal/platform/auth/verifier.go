package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

const (
	maxAccessTokenLength = 16_384
	jwtClockSkewSeconds  = 60
	maxJtiLength         = 128
	accessTokenType      = "access"
)

// Principal identifies the authenticated caller behind an access token.
type Principal struct {
	UserID        string
	SessionID     string
	EmailVerified bool
	Roles         []string
}

// ErrInvalidToken reports an unusable access token.
var ErrInvalidToken = errors.New("auth: invalid access token")

// Verifier validates access tokens against the keyring.
type Verifier struct {
	ring     *KeyRing
	clock    clock.Clock
	issuer   string
	audience string
}

// NewVerifier builds an access-token verifier; production-like environments
// require the issuer and audience claims to be configured.
func NewVerifier(ring *KeyRing, clk clock.Clock, issuer, audience string, productionLike bool) (*Verifier, error) {
	issuer = strings.TrimSpace(issuer)
	audience = strings.TrimSpace(audience)
	if productionLike && (issuer == "" || audience == "") {
		return nil, fmt.Errorf("auth: AUTH_ISSUER and AUTH_AUDIENCE are required when APP_ENV is staging or production")
	}
	return &Verifier{ring: ring, clock: clk, issuer: issuer, audience: audience}, nil
}

// VerifyAccessToken verifies the token signature and claims, returning the principal.
func (v *Verifier) VerifyAccessToken(token string) (Principal, error) {
	payload, err := v.verifiedPayload(token)
	if err != nil {
		return Principal{}, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Principal{}, ErrInvalidToken
	}
	return v.principalFromClaims(claims)
}

func (v *Verifier) verifiedPayload(token string) ([]byte, error) {
	if len(token) > maxAccessTokenLength {
		return nil, ErrInvalidToken
	}
	message, err := jws.Parse([]byte(token))
	if err != nil {
		return nil, ErrInvalidToken
	}
	signatures := message.Signatures()
	if len(signatures) == 0 {
		return nil, ErrInvalidToken
	}
	headers := signatures[0].ProtectedHeaders()
	kid, ok := headers.KeyID()
	if !ok || strings.TrimSpace(kid) == "" {
		return nil, ErrInvalidToken
	}
	alg, ok := headers.Algorithm()
	if !ok || alg.String() != jwa.EdDSA().String() {
		return nil, ErrInvalidToken
	}
	publicKey, ok := v.ring.PublicKeyForKid(kid)
	if !ok {
		return nil, ErrInvalidToken
	}
	if _, err := jws.Verify([]byte(token), jws.WithKey(jwa.EdDSA(), publicKey)); err != nil {
		return nil, ErrInvalidToken
	}
	return message.Payload(), nil
}

// principalFromClaims reads a map because aud is a string or an array.
// accessTokenPayload can only emit a string.
func (v *Verifier) principalFromClaims(claims map[string]any) (Principal, error) {
	nowSeconds := float64(v.clock.Now().Unix())
	issuedAt, ok := numberClaim(claims, "iat")
	if !ok || issuedAt > nowSeconds+jwtClockSkewSeconds {
		return Principal{}, ErrInvalidToken
	}
	expiresAt, ok := numberClaim(claims, "exp")
	if !ok || expiresAt <= nowSeconds {
		return Principal{}, ErrInvalidToken
	}
	if issuedAt > expiresAt {
		return Principal{}, ErrInvalidToken
	}
	if notBefore, ok := numberClaim(claims, "nbf"); ok {
		if notBefore > nowSeconds+jwtClockSkewSeconds || notBefore > expiresAt {
			return Principal{}, ErrInvalidToken
		}
	}
	if v.issuer != "" && claims["iss"] != v.issuer {
		return Principal{}, ErrInvalidToken
	}
	if v.audience != "" && !audienceMatches(claims["aud"], v.audience) {
		return Principal{}, ErrInvalidToken
	}
	if claims["typ"] != accessTokenType {
		return Principal{}, ErrInvalidToken
	}
	userID, _ := claims["sub"].(string)
	sessionID, _ := claims["sid"].(string)
	jti, _ := claims["jti"].(string)
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(jti) == "" {
		return Principal{}, ErrInvalidToken
	}
	if len(jti) > maxJtiLength {
		return Principal{}, ErrInvalidToken
	}
	emailVerified, _ := claims["email_verified"].(bool)
	return Principal{
		UserID:        userID,
		SessionID:     sessionID,
		EmailVerified: emailVerified,
		Roles:         parseRoles(claims["roles"]),
	}, nil
}

func numberClaim(claims map[string]any, name string) (float64, bool) {
	value, ok := claims[name].(float64)
	return value, ok
}

func audienceMatches(value any, expected string) bool {
	switch audience := value.(type) {
	case string:
		return audience == expected
	case []any:
		for _, item := range audience {
			if item == expected {
				return true
			}
		}
	}
	return false
}

func parseRoles(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	roles := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		role, ok := item.(string)
		if !ok {
			continue
		}
		role = strings.TrimSpace(role)
		if role == "" || seen[role] {
			continue
		}
		seen[role] = true
		roles = append(roles, role)
	}
	return roles
}
