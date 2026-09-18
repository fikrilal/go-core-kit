package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
)

func guardServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	keysJSON, _ := testKeyPair(t)
	ring := testVerifierRing(t, keysJSON)
	token, err := platformauth.NewIssuer(ring, clock.Fixed{Moment: fixedMoment}, "", "", 15*time.Minute).SignAccessToken(
		platformauth.AccessClaims{UserID: "user-1", SessionID: "session-1", EmailVerified: true, Roles: []string{"USER"}},
	)
	if err != nil {
		t.Fatalf("SignAccessToken() error = %v", err)
	}
	verifier, err := platformauth.NewVerifier(ring, clock.Fixed{Moment: fixedMoment}, "", "", false)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	guard := platformauth.NewGuard(verifier)

	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	router := httpx.NewRouter(logger)
	protected := guard.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := platformauth.PrincipalFrom(r.Context())
		if !ok {
			http.Error(w, "missing principal", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(principal)
	}))
	router.HandleFunc(
		httpx.Operation{Method: http.MethodGet, Path: "/protected", ID: "test.protected"},
		protected.ServeHTTP,
	)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, token
}

func TestGuardAttachesPrincipal(t *testing.T) {
	server, token := guardServer(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var principal struct {
		UserID        string   `json:"UserID"`
		SessionID     string   `json:"SessionID"`
		EmailVerified bool     `json:"EmailVerified"`
		Roles         []string `json:"Roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&principal); err != nil {
		t.Fatalf("decode principal: %v", err)
	}
	if principal.UserID != "user-1" || principal.SessionID != "session-1" || !principal.EmailVerified {
		t.Errorf("principal = %+v, want the token claims", principal)
	}
}

func TestGuardRejectsUnauthorized(t *testing.T) {
	server, token := guardServer(t)
	cases := []struct {
		name   string
		header string
	}{
		{name: "missing header", header: ""},
		{name: "wrong scheme", header: "Basic abc"},
		{name: "scheme without token", header: "Bearer"},
		{name: "tampered token", header: "Bearer " + tamperedToken(token)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertUnauthorized(t, server, tc.header)
		})
	}
}

func assertUnauthorized(t *testing.T, server *httptest.Server, header string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var problem struct {
		Code    string `json:"code"`
		TraceID string `json:"traceId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "UNAUTHORIZED" {
		t.Errorf("code = %q, want UNAUTHORIZED", problem.Code)
	}
	if problem.TraceID == "" {
		t.Error("problem has no traceId")
	}
}
