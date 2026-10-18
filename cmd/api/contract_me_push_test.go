package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

const pushTokenFixture = "fcm-registration-token-1"

func withPush(t *testing.T) *authFixture {
	t.Helper()
	return newAuthFixtureWith(t, func(cfg *config.Config) {
		cfg.FCMProjectID = "demo"
		cfg.FCMServiceAccount = `{"client_email":"worker@demo.iam"}`
	})
}

func (f *authFixture) upsertPushToken(t *testing.T, token string, body any) (*http.Request, contracttest.Response) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, f.baseURL+"/v1/me/push-token", bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, contracttest.Do(t, f.client, req)
}

func (f *authFixture) revokePushToken(t *testing.T, token string) (*http.Request, contracttest.Response) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, f.baseURL+"/v1/me/push-token", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, contracttest.Do(t, f.client, req)
}

func sessionIDForDevice(t *testing.T, f *authFixture, deviceID string) string {
	t.Helper()
	var id string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT id FROM sessions WHERE device_id = ?"), deviceID).Scan(&id); err != nil {
		t.Fatalf("session for %s: %v", deviceID, err)
	}
	return id
}

func storedPushToken(t *testing.T, f *authFixture, sessionID string) (platform, token, revokedAt *string) {
	t.Helper()
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT push_platform, push_token, push_token_revoked_at FROM sessions WHERE id = ?"), sessionID).
		Scan(&platform, &token, &revokedAt); err != nil {
		t.Fatalf("push columns: %v", err)
	}
	return platform, token, revokedAt
}

func TestContractUsersMePushTokenNotConfigured(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "push-off@example.com", "device-a")
	token := registered.Data.AccessToken

	_, resp := f.upsertPushToken(t, token, map[string]any{"platform": "ANDROID", "token": pushTokenFixture})
	if resp.Status != http.StatusNotImplemented {
		t.Fatalf("upsert status = %d, want 501\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "AUTH_PUSH_NOT_CONFIGURED")
	f.suite.AssertErrorCodeDeclared(t, "users.me.pushToken.upsert", "AUTH_PUSH_NOT_CONFIGURED")

	if _, resp := f.revokePushToken(t, token); resp.Status != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204 without the push gate", resp.Status)
	}
}

func TestContractUsersMePushTokenUpsert(t *testing.T) {
	f := withPush(t)
	registered := f.register(t, "push@example.com", "device-a")
	token := registered.Data.AccessToken
	sessionID := sessionIDForDevice(t, f, "device-a")

	req, resp := f.upsertPushToken(t, token, map[string]any{"platform": "ios", "token": pushTokenFixture})
	if resp.Status != http.StatusNoContent {
		t.Fatalf("upsert status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)
	if len(resp.Body) != 0 {
		t.Errorf("upsert body = %q, want empty", resp.Body)
	}

	platform, stored, revokedAt := storedPushToken(t, f, sessionID)
	if platform == nil || *platform != "IOS" {
		t.Errorf("push_platform = %v, want IOS (platform is uppercased)", platform)
	}
	if stored == nil || *stored != pushTokenFixture {
		t.Errorf("push_token = %v, want the registered token", stored)
	}
	if revokedAt != nil {
		t.Errorf("push_token_revoked_at = %q, want NULL after a fresh registration", *revokedAt)
	}

	if _, resp := f.upsertPushToken(t, token, map[string]any{"platform": "IOS", "token": pushTokenFixture}); resp.Status != http.StatusNoContent {
		t.Fatalf("repeat upsert status = %d, want 204 (idempotent replacement)", resp.Status)
	}
}

func TestContractUsersMePushTokenMovesBetweenSessions(t *testing.T) {
	f := withPush(t)
	registered := f.register(t, "switch@example.com", "device-a")
	second := f.login(t, "switch@example.com", testPassword, "device-b")
	firstToken := registered.Data.AccessToken
	secondToken := decodeAuthResponse(t, second.Body).Data.AccessToken
	firstSession := sessionIDForDevice(t, f, "device-a")
	secondSession := sessionIDForDevice(t, f, "device-b")

	if _, resp := f.upsertPushToken(t, firstToken, map[string]any{"platform": "ANDROID", "token": pushTokenFixture}); resp.Status != http.StatusNoContent {
		t.Fatalf("first upsert status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	if _, resp := f.upsertPushToken(t, secondToken, map[string]any{"platform": "ANDROID", "token": pushTokenFixture}); resp.Status != http.StatusNoContent {
		t.Fatalf("second upsert status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}

	platform, stored, revokedAt := storedPushToken(t, f, firstSession)
	if platform != nil || stored != nil {
		t.Errorf("first session still holds platform %v token %v, want cleared", platform, stored)
	}
	if revokedAt == nil {
		t.Error("first session push_token_revoked_at = NULL, want the hand-off stamp")
	}
	platform, stored, revokedAt = storedPushToken(t, f, secondSession)
	if platform == nil || *platform != "ANDROID" || stored == nil || *stored != pushTokenFixture {
		t.Errorf("second session = %v/%v, want the moved token", platform, stored)
	}
	if revokedAt != nil {
		t.Error("second session push_token_revoked_at set, want NULL")
	}
}

func TestContractUsersMePushTokenDeadSession(t *testing.T) {
	f := withPush(t)
	first := f.register(t, "dead-session@example.com", "device-a")
	f.login(t, "dead-session@example.com", testPassword, "device-b")
	keeper := f.login(t, "dead-session@example.com", testPassword, "device-c")
	keeperToken := decodeAuthResponse(t, keeper.Body).Data.AccessToken
	keeperSession := sessionIDForDevice(t, f, "device-c")
	if _, resp := f.upsertPushToken(t, keeperToken, map[string]any{"platform": "WEB", "token": pushTokenFixture}); resp.Status != http.StatusNoContent {
		t.Fatalf("keeper upsert status = %d, want 204", resp.Status)
	}

	deadSession := sessionIDForDevice(t, f, "device-a")
	if _, resp := f.revokeSession(t, keeperToken, deadSession); resp.Status != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204", resp.Status)
	}

	_, resp := f.upsertPushToken(t, first.Data.AccessToken, map[string]any{"platform": "ANDROID", "token": pushTokenFixture})
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a revoked session\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "UNAUTHORIZED")

	platform, stored, _ := storedPushToken(t, f, keeperSession)
	if platform == nil || *platform != "WEB" || stored == nil {
		t.Error("a dead session detached the live session's push token")
	}
}

func TestContractUsersMePushTokenValidation(t *testing.T) {
	f := withPush(t)
	registered := f.register(t, "push-invalid@example.com", "")
	token := registered.Data.AccessToken
	longToken := strings.Repeat("a", 2049)

	cases := []struct {
		name    string
		body    any
		field   string
		message string
	}{
		{name: "missing fields", body: map[string]any{}, field: "platform", message: "is required"},
		{name: "unknown platform", body: map[string]any{"platform": "WATCH", "token": pushTokenFixture}, field: "platform", message: `must be one of "ANDROID", "IOS", "WEB"`},
		{name: "blank token", body: map[string]any{"platform": "ANDROID", "token": "   "}, field: "token", message: "must not be empty"},
		{name: "over long token", body: map[string]any{"platform": "ANDROID", "token": longToken}, field: "token", message: "must be at most 2048 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp := f.upsertPushToken(t, token, tc.body)
			if resp.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
			f.suite.AssertErrorCodeDeclared(t, "users.me.pushToken.upsert", "VALIDATION_FAILED")
			issues := decodeMeIssues(t, resp.Body)
			if len(issues.Errors) == 0 || issues.Errors[0].Field != tc.field {
				t.Fatalf("errors = %+v, want the %s issue first", issues.Errors, tc.field)
			}
			if tc.message != "" && issues.Errors[0].Message != tc.message {
				t.Errorf("message = %q, want %q", issues.Errors[0].Message, tc.message)
			}
		})
	}
}

func TestContractUsersMePushTokenRevoke(t *testing.T) {
	f := withPush(t)
	registered := f.register(t, "push-revoke@example.com", "device-a")
	token := registered.Data.AccessToken
	sessionID := sessionIDForDevice(t, f, "device-a")

	if _, resp := f.upsertPushToken(t, token, map[string]any{"platform": "ANDROID", "token": pushTokenFixture}); resp.Status != http.StatusNoContent {
		t.Fatalf("upsert status = %d, want 204", resp.Status)
	}
	req, resp := f.revokePushToken(t, token)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	if len(resp.Body) != 0 {
		t.Errorf("revoke body = %q, want empty", resp.Body)
	}
	platform, stored, revokedAt := storedPushToken(t, f, sessionID)
	if platform != nil || stored != nil {
		t.Errorf("session still holds platform %v token %v, want cleared", platform, stored)
	}
	if revokedAt == nil {
		t.Error("push_token_revoked_at = NULL, want the revoke stamp")
	}

	if _, resp := f.revokePushToken(t, token); resp.Status != http.StatusNoContent {
		t.Fatalf("repeat revoke status = %d, want 204", resp.Status)
	}
}

func TestContractUsersMePushTokenUnauthorized(t *testing.T) {
	f := withPush(t)

	t.Run("missing token", func(t *testing.T) {
		_, upsertResp := f.upsertPushToken(t, "", map[string]any{"platform": "ANDROID", "token": pushTokenFixture})
		if upsertResp.Status != http.StatusUnauthorized {
			t.Fatalf("upsert status = %d, want 401\nbody: %s", upsertResp.Status, upsertResp.Body)
		}
		contracttest.AssertProblem(t, upsertResp, "UNAUTHORIZED")

		_, revokeResp := f.revokePushToken(t, "")
		if revokeResp.Status != http.StatusUnauthorized {
			t.Fatalf("revoke status = %d, want 401\nbody: %s", revokeResp.Status, revokeResp.Body)
		}
		contracttest.AssertProblem(t, revokeResp, "UNAUTHORIZED")
		f.suite.AssertErrorCodeDeclared(t, "users.me.pushToken.upsert", "UNAUTHORIZED")
		f.suite.AssertErrorCodeDeclared(t, "users.me.pushToken.revoke", "UNAUTHORIZED")
	})

	t.Run("deleted user", func(t *testing.T) {
		leaving := f.register(t, "push-deleted@example.com", "")
		if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = 'DELETED' WHERE id = ?"), leaving.Data.User.ID); err != nil {
			t.Fatalf("delete user: %v", err)
		}
		_, resp := f.upsertPushToken(t, leaving.Data.AccessToken, map[string]any{"platform": "ANDROID", "token": pushTokenFixture})
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("upsert status = %d, want 401\nbody: %s", resp.Status, resp.Body)
		}
		_, resp = f.revokePushToken(t, leaving.Data.AccessToken)
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("revoke status = %d, want 401\nbody: %s", resp.Status, resp.Body)
		}
	})
}
