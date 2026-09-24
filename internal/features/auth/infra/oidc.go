package infra

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/fikrilal/go-core-kit/internal/features/auth/domain"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// googleJWKSURL is Google's published certificate endpoint.
const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// GoogleIDTokenVerifier verifies Google ID tokens against the published JWKS.
type GoogleIDTokenVerifier struct {
	clientIDs []string
	jwksURL   string
	client    *http.Client
	clock     clock.Clock

	mu        sync.Mutex
	keys      jwk.Set
	fetchedAt time.Time
}

var googleIssuers = map[string]bool{
	"https://accounts.google.com": true,
	"accounts.google.com":         true,
}

// NewGoogleIDTokenVerifier builds the verifier; clientIDs is a comma-separated
// list and jwksURL overrides Google's certificate endpoint when non-empty.
func NewGoogleIDTokenVerifier(clientIDs, jwksURL string, clk clock.Clock) *GoogleIDTokenVerifier {
	if strings.TrimSpace(jwksURL) == "" {
		jwksURL = googleJWKSURL
	}
	return &GoogleIDTokenVerifier{
		clientIDs: parseClientIDs(clientIDs),
		jwksURL:   jwksURL,
		client:    &http.Client{Timeout: 10 * time.Second},
		clock:     clk,
	}
}

// VerifyIDToken verifies a Google ID token's signature, issuer, audience, and expiry.
// A failure, including a JWKS outage, is the invalid kind.
// A string email_verified is intentional and is not how access tokens work.
func (v *GoogleIDTokenVerifier) VerifyIDToken(ctx context.Context, provider, idToken string) (domain.OidcVerification, error) {
	if provider != domain.AuthMethodGoogle {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	if len(v.clientIDs) == 0 {
		return domain.OidcVerification{Kind: domain.OidcVerifyNotConfigured}, nil
	}
	keys, err := v.keySet(ctx)
	if err != nil {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil //nolint:nilerr // a failed verification is the invalid outcome, not a transport error
	}
	if _, err := jws.Verify([]byte(idToken), jws.WithKeySet(keys), jws.WithValidateKey(true)); err != nil {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil //nolint:nilerr // a failed verification is the invalid outcome, not a transport error
	}
	token, err := jwt.Parse([]byte(idToken), jwt.WithVerify(false), jwt.WithValidate(false))
	if err != nil {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil //nolint:nilerr // a failed verification is the invalid outcome, not a transport error
	}
	issuer, ok := token.Issuer()
	if !ok || !googleIssuers[issuer] {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	audience, ok := token.Audience()
	if !ok || !audienceAllowed(audience, v.clientIDs) {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	expiresAt, ok := token.Expiration()
	if !ok || expiresAt.IsZero() || !expiresAt.After(v.clock.Now()) {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	subject, ok := token.Subject()
	if !ok {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	var email string
	if err := token.Get("email", &email); err != nil {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil //nolint:nilerr // a missing claim is the invalid outcome, not a transport error
	}
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(email) == "" {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	var verifiedValue any
	if err := token.Get("email_verified", &verifiedValue); err != nil {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil //nolint:nilerr // a missing claim is the invalid outcome, not a transport error
	}
	emailVerified, ok := parseEmailVerified(verifiedValue)
	if !ok {
		return domain.OidcVerification{Kind: domain.OidcVerifyInvalid}, nil
	}
	displayName := optionalStringClaim(token, "name")
	givenName := optionalStringClaim(token, "given_name")
	familyName := optionalStringClaim(token, "family_name")
	return domain.OidcVerification{
		Kind: domain.OidcVerifyVerified,
		Identity: domain.OidcIdentity{
			Provider:      provider,
			Subject:       subject,
			Email:         email,
			EmailVerified: emailVerified,
			DisplayName:   displayName,
			GivenName:     givenName,
			FamilyName:    familyName,
		},
	}, nil
}

// keySet returns the cached JWKS. The mutex is held across the fetch.
func (v *GoogleIDTokenVerifier) keySet(ctx context.Context) (jwk.Set, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.keys != nil && v.clock.Now().Sub(v.fetchedAt) < googleJWKSCacheTTL {
		return v.keys, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	response, err := v.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc jwks: unexpected status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	set, err := jwk.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	v.keys, v.fetchedAt = set, v.clock.Now()
	return set, nil
}

const googleJWKSCacheTTL = 15 * time.Minute

func parseClientIDs(raw string) []string {
	ids := make([]string, 0, 2)
	for _, id := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}
	return ids
}

func parseEmailVerified(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

func audienceAllowed(audience []string, allowed []string) bool {
	for _, id := range audience {
		if slices.Contains(allowed, id) {
			return true
		}
	}
	return false
}

func optionalStringClaim(token jwt.Token, name string) string {
	var value string
	if err := token.Get(name, &value); err != nil {
		return ""
	}
	return value
}
