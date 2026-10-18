package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

const changedPassword = "brand-new-horse-battery"

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (f *authFixture) changePassword(t *testing.T, token, idempotencyKey string, body any) (*http.Request, contracttest.Response) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.baseURL+"/v1/auth/password/change", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return req, contracttest.Do(t, f.client, req)
}

func TestContractAuthChangePasswordHappyPath(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "change@example.com", "device-a")
	token := registered.Data.AccessToken

	secondResp := f.login(t, "change@example.com", testPassword, "device-b")
	if secondResp.Status != http.StatusOK {
		t.Fatalf("second login status = %d, want 200", secondResp.Status)
	}
	second := decodeAuthResponse(t, secondResp.Body)

	req, resp := f.changePassword(t, token, "change-key-1", changePasswordRequest{CurrentPassword: testPassword, NewPassword: changedPassword})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("change status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)
	if len(resp.Body) != 0 {
		t.Errorf("change body = %q, want empty", resp.Body)
	}

	replayReq, replay := f.changePassword(t, token, "change-key-1", changePasswordRequest{CurrentPassword: testPassword, NewPassword: changedPassword})
	if replay.Status != http.StatusNoContent {
		t.Fatalf("replay status = %d, want the cached 204\nbody: %s", replay.Status, replay.Body)
	}
	if replayed := replay.Header.Get("Idempotency-Replayed"); replayed != "true" {
		t.Errorf("replay header = %q, want Idempotency-Replayed: true", replayed)
	}
	_ = replayReq

	if resp := f.login(t, "change@example.com", testPassword, ""); resp.Status != http.StatusUnauthorized {
		t.Fatalf("old password login status = %d, want 401", resp.Status)
	}
	if resp := f.login(t, "change@example.com", changedPassword, ""); resp.Status != http.StatusOK {
		t.Fatalf("new password login status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}

	_, resp = f.refresh(t, second.Data.RefreshToken)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("other session refresh status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_SESSION_REVOKED")

	_, resp = f.refresh(t, registered.Data.RefreshToken)
	if resp.Status != http.StatusOK {
		t.Fatalf("current session refresh status = %d, want 200 (current session kept)\nbody: %s", resp.Status, resp.Body)
	}
}

func TestContractAuthChangePasswordErrors(t *testing.T) {
	newFixtureWithUser := func(t *testing.T) (*authFixture, string) {
		t.Helper()
		f := newAuthFixture(t)
		registered := f.register(t, "guard@example.com", "")
		return f, registered.Data.AccessToken
	}
	body := changePasswordRequest{CurrentPassword: testPassword, NewPassword: changedPassword}

	t.Run("missing token", func(t *testing.T) {
		f, _ := newFixtureWithUser(t)
		_, resp := f.changePassword(t, "", "", body)
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
		f.suite.AssertErrorCodeDeclared(t, "auth.password.change", "UNAUTHORIZED")
	})

	t.Run("wrong current password", func(t *testing.T) {
		f, token := newFixtureWithUser(t)
		_, resp := f.changePassword(t, token, "", changePasswordRequest{CurrentPassword: "definitely-wrong", NewPassword: changedPassword})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "AUTH_CURRENT_PASSWORD_INVALID")
		f.suite.AssertErrorCodeDeclared(t, "auth.password.change", "AUTH_CURRENT_PASSWORD_INVALID")
	})

	t.Run("identical passwords", func(t *testing.T) {
		f, token := newFixtureWithUser(t)
		_, resp := f.changePassword(t, token, "", changePasswordRequest{CurrentPassword: testPassword, NewPassword: testPassword})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
		var problem struct {
			Errors []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(resp.Body, &problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if len(problem.Errors) != 1 || problem.Errors[0].Field != "newPassword" || problem.Errors[0].Message != "New password must be different" {
			t.Errorf("errors = %+v, want the different-password issue", problem.Errors)
		}
	})

	t.Run("short new password", func(t *testing.T) {
		f, token := newFixtureWithUser(t)
		_, resp := f.changePassword(t, token, "", changePasswordRequest{CurrentPassword: testPassword, NewPassword: "short"})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
	})

	t.Run("missing fields", func(t *testing.T) {
		f, token := newFixtureWithUser(t)
		_, resp := f.changePassword(t, token, "", map[string]any{})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
	})

	t.Run("password not set", func(t *testing.T) {
		f, token := newFixtureWithUser(t)
		if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("DELETE FROM password_credentials")); err != nil {
			t.Fatalf("delete credential: %v", err)
		}
		_, resp := f.changePassword(t, token, "", body)
		if resp.Status != http.StatusConflict {
			t.Fatalf("status = %d, want 409\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "AUTH_PASSWORD_NOT_SET")
		f.suite.AssertErrorCodeDeclared(t, "auth.password.change", "AUTH_PASSWORD_NOT_SET")
	})
}

func TestContractAuthChangePasswordGuardPrecedesIdempotency(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "outer@example.com", "")
	token := registered.Data.AccessToken
	body := changePasswordRequest{CurrentPassword: testPassword, NewPassword: changedPassword}

	_, resp := f.changePassword(t, "", "outer-key", body)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", resp.Status)
	}

	_, resp = f.changePassword(t, token, "outer-key", body)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("authorized status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	if replayed := resp.Header.Get("Idempotency-Replayed"); replayed != "" {
		t.Errorf("Idempotency-Replayed = %q, want empty: the unauthorized request must not prime the store", replayed)
	}
}
