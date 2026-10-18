package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type refreshResponse struct {
	Data struct {
		User struct {
			ID            string `json:"id"`
			Email         string `json:"email"`
			EmailVerified bool   `json:"emailVerified"`
		} `json:"user"`
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	} `json:"data"`
}

func (f *authFixture) refresh(t *testing.T, token string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.post(t, "/v1/auth/refresh", refreshRequest{RefreshToken: token})
}

func TestContractAuthRefreshRotates(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "rotate@example.com", "device-a")
	oldRefresh := registered.Data.RefreshToken
	userID := registered.Data.User.ID

	req, resp := f.refresh(t, oldRefresh)
	if resp.Status != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)

	var raw map[string]any
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		t.Fatalf("decode raw body: %v", err)
	}
	data, _ := raw["data"].(map[string]any)
	user, _ := data["user"].(map[string]any)
	if _, present := user["authMethods"]; present {
		t.Errorf("refresh response user includes authMethods; the reference omits it on refresh: %s", resp.Body)
	}

	var decoded refreshResponse
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	if decoded.Data.User.ID != userID || decoded.Data.User.Email != "rotate@example.com" {
		t.Errorf("user = %+v, want the registered user", decoded.Data.User)
	}
	if decoded.Data.RefreshToken == oldRefresh {
		t.Error("refresh token was not rotated")
	}
	assertAccessToken(t, decoded.Data.AccessToken, userID, false)

	var revokedAt *string
	var replacedByID *string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT revoked_at, replaced_by_id FROM refresh_tokens WHERE token_hash = ?"), hashRefreshTokenValue(oldRefresh)).Scan(&revokedAt, &replacedByID); err != nil {
		t.Fatalf("old token row: %v", err)
	}
	if revokedAt == nil || replacedByID == nil {
		t.Fatalf("old token revoked/replacedBy = %v/%v, want both set", revokedAt, replacedByID)
	}
	var successorHash string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT token_hash FROM refresh_tokens WHERE id = ?"), *replacedByID).Scan(&successorHash); err != nil {
		t.Fatalf("successor token row: %v", err)
	}
	if successorHash != hashRefreshTokenValue(decoded.Data.RefreshToken) {
		t.Error("replaced_by_id does not point at the newly issued token")
	}
}

func TestContractAuthRefreshReuseContainment(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "reuse@example.com", "device-a")
	first := registered.Data.RefreshToken

	_, resp := f.refresh(t, first)
	if resp.Status != http.StatusOK {
		t.Fatalf("first refresh status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	var rotated refreshResponse
	if err := json.Unmarshal(resp.Body, &rotated); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}

	_, resp = f.refresh(t, first)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("reused token status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_REFRESH_TOKEN_REUSED")
	f.suite.AssertErrorCodeDeclared(t, "auth.refresh", "AUTH_REFRESH_TOKEN_REUSED")

	_, resp = f.refresh(t, rotated.Data.RefreshToken)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("successor after reuse status = %d, want 401 (family revoked)\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_SESSION_REVOKED")
}

func TestContractAuthRefreshExpired(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "expired@example.com", "")

	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE refresh_tokens SET expires_at = ? WHERE token_hash = ?"), "2000-01-01T00:00:00.000000000Z", hashRefreshTokenValue(registered.Data.RefreshToken)); err != nil {
		t.Fatalf("expire token: %v", err)
	}
	_, resp := f.refresh(t, registered.Data.RefreshToken)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_REFRESH_TOKEN_EXPIRED")
	f.suite.AssertErrorCodeDeclared(t, "auth.refresh", "AUTH_REFRESH_TOKEN_EXPIRED")
}

func TestContractAuthRefreshInvalidAndValidation(t *testing.T) {
	f := newAuthFixture(t)

	t.Run("unknown token", func(t *testing.T) {
		_, resp := f.refresh(t, "not-a-real-token")
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "AUTH_REFRESH_TOKEN_INVALID")
		f.suite.AssertErrorCodeDeclared(t, "auth.refresh", "AUTH_REFRESH_TOKEN_INVALID")
	})

	t.Run("missing token", func(t *testing.T) {
		_, resp := f.post(t, "/v1/auth/refresh", map[string]any{})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
	})
}

func TestContractAuthRefreshSuspensionAndDeletion(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "moderated2@example.com", "")
	token := registered.Data.RefreshToken

	update := func(status string) {
		t.Helper()
		if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = ? WHERE email = ?"), status, "moderated2@example.com"); err != nil {
			t.Fatalf("update status: %v", err)
		}
	}

	update("SUSPENDED")
	_, resp := f.refresh(t, token)
	if resp.Status != http.StatusForbidden {
		t.Fatalf("suspended refresh status = %d, want 403\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_USER_SUSPENDED")

	update("DELETED")
	_, resp = f.refresh(t, token)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("deleted refresh status = %d, want 401 (masked)\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_REFRESH_TOKEN_INVALID")
}

func TestContractAuthLogout(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "logout@example.com", "device-a")
	token := registered.Data.RefreshToken

	req, resp := f.post(t, "/v1/auth/logout", refreshRequest{RefreshToken: token})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)
	if len(resp.Body) != 0 {
		t.Errorf("logout body = %q, want empty", resp.Body)
	}

	var sessionRevoked *string
	var tokensRevoked int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT revoked_at, active_key FROM sessions WHERE user_id = ?"), registered.Data.User.ID).Scan(&sessionRevoked, new(*string)); err != nil {
		t.Fatalf("session row: %v", err)
	}
	if sessionRevoked == nil {
		t.Error("session not revoked after logout")
	}
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM refresh_tokens WHERE revoked_at IS NULL")).Scan(&tokensRevoked); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if tokensRevoked != 0 {
		t.Errorf("active refresh tokens = %d, want 0 after logout", tokensRevoked)
	}

	_, resp = f.refresh(t, token)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_SESSION_REVOKED")

	_, resp = f.post(t, "/v1/auth/logout", refreshRequest{RefreshToken: token})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("second logout status = %d, want 204 (revocation is idempotent)\nbody: %s", resp.Status, resp.Body)
	}

	_, resp = f.post(t, "/v1/auth/logout", refreshRequest{RefreshToken: "unknown-token"})
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("logout unknown token status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_REFRESH_TOKEN_INVALID")
	f.suite.AssertErrorCodeDeclared(t, "auth.logout", "AUTH_REFRESH_TOKEN_INVALID")
}
