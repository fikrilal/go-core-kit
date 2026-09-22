package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

type principalContextKey struct{}

// WithPrincipal attaches the authenticated principal to a request context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFrom returns the principal the guard attached to the context.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// Guard authenticates requests with bearer access tokens.
type Guard struct {
	verifier *Verifier
}

// NewGuard builds the bearer-token guard.
func NewGuard(verifier *Verifier) *Guard {
	return &Guard{verifier: verifier}
}

// Require wraps next, rejecting requests without a valid access token.
func (g *Guard) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			httpx.WriteError(w, r, unauthorized())
			return
		}
		principal, err := g.verifier.VerifyAccessToken(token)
		if err != nil {
			if errors.Is(err, ErrInvalidToken) {
				httpx.WriteError(w, r, unauthorized())
				return
			}
			// A client token never reaches this branch. It is a broken verifier.
			httpx.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
	})
}

func unauthorized() *httpx.ProblemError {
	return &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized}
}

func bearerToken(header string) string {
	trimmed := strings.TrimSpace(header)
	index := strings.IndexFunc(trimmed, unicode.IsSpace)
	if index < 0 {
		return ""
	}
	if !strings.EqualFold(trimmed[:index], "bearer") {
		return ""
	}
	return strings.TrimSpace(trimmed[index:])
}
