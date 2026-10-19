package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

type meEnvelope struct {
	Data struct {
		ID            string   `json:"id"`
		Email         string   `json:"email"`
		EmailVerified bool     `json:"emailVerified"`
		Roles         []string `json:"roles"`
		AuthMethods   []string `json:"authMethods"`
		Profile       struct {
			DisplayName *string `json:"displayName"`
			GivenName   *string `json:"givenName"`
			FamilyName  *string `json:"familyName"`
		} `json:"profile"`
	} `json:"data"`
}

type meIssues struct {
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func decodeMe(t *testing.T, body []byte) meEnvelope {
	t.Helper()
	var decoded meEnvelope
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	return decoded
}

func decodeMeIssues(t *testing.T, body []byte) meIssues {
	t.Helper()
	var decoded meIssues
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode problems: %v", err)
	}
	return decoded
}

func (f *authFixture) meRequest(t *testing.T, method, token, idempotencyKey string, body any) (*http.Request, contracttest.Response) {
	t.Helper()
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		payload = encoded
	}
	req, err := http.NewRequestWithContext(t.Context(), method, f.baseURL+"/v1/me", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return req, contracttest.Do(t, f.client, req)
}

func (f *authFixture) getMe(t *testing.T, token string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.meRequest(t, http.MethodGet, token, "", nil)
}

func (f *authFixture) patchMe(t *testing.T, token, idempotencyKey string, body any) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.meRequest(t, http.MethodPatch, token, idempotencyKey, body)
}

func profilePatch(profile map[string]any) map[string]any {
	return map[string]any{"profile": profile}
}

func storedProfileName(t *testing.T, f *authFixture, userID string) *string {
	t.Helper()
	var name *string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT display_name FROM user_profiles WHERE user_id = ?"), userID).Scan(&name); err != nil {
		t.Fatalf("profile row: %v", err)
	}
	return name
}

func TestContractUsersMeGet(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "me@example.com", "")

	req, resp := f.getMe(t, registered.Data.AccessToken)
	if resp.Status != http.StatusOK {
		t.Fatalf("me status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	assertGoldenShapePath(t, req, resp.Status, resp.Body)
	contracttest.AssertRequestID(t, resp)

	me := decodeMe(t, resp.Body)
	if me.Data.ID != registered.Data.User.ID || me.Data.Email != "me@example.com" {
		t.Errorf("me = %+v, want the registered user", me.Data)
	}
	if me.Data.EmailVerified {
		t.Error("emailVerified = true, want false for a password account")
	}
	if len(me.Data.Roles) != 1 || me.Data.Roles[0] != "USER" {
		t.Errorf("roles = %v, want [USER]", me.Data.Roles)
	}
	if len(me.Data.AuthMethods) != 1 || me.Data.AuthMethods[0] != "PASSWORD" {
		t.Errorf("authMethods = %v, want [PASSWORD]", me.Data.AuthMethods)
	}
	if me.Data.Profile.DisplayName != nil || me.Data.Profile.GivenName != nil || me.Data.Profile.FamilyName != nil {
		t.Errorf("profile = %+v, want null fields for a user without a profile", me.Data.Profile)
	}
}

func TestContractUsersMeGetUnauthorized(t *testing.T) {
	f := newAuthFixture(t)
	_, resp := f.getMe(t, "")
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
	f.suite.AssertErrorCodeDeclared(t, "users.me.get", "UNAUTHORIZED")

	registered := f.register(t, "gone@example.com", "")
	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = 'DELETED' WHERE id = ?"), registered.Data.User.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	_, resp = f.getMe(t, registered.Data.AccessToken)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("deleted status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
}

func TestContractUsersMePatchSetsTrimsAndClears(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "patch@example.com", "")
	token := registered.Data.AccessToken

	req, resp := f.patchMe(t, token, "me-key-1", profilePatch(map[string]any{"displayName": "  Dante  "}))
	if resp.Status != http.StatusOK {
		t.Fatalf("patch status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	assertGoldenShapePath(t, req, resp.Status, resp.Body)
	contracttest.AssertRequestID(t, resp)
	me := decodeMe(t, resp.Body)
	if me.Data.Profile.DisplayName == nil || *me.Data.Profile.DisplayName != "Dante" {
		t.Errorf("displayName = %v, want the trimmed Dante", me.Data.Profile.DisplayName)
	}
	if stored := storedProfileName(t, f, registered.Data.User.ID); stored == nil || *stored != "Dante" {
		t.Errorf("stored displayName = %v, want Dante", stored)
	}

	replayReq, replay := f.patchMe(t, token, "me-key-1", profilePatch(map[string]any{"displayName": "  Dante  "}))
	if replay.Status != http.StatusOK {
		t.Fatalf("replay status = %d, want the cached 200\nbody: %s", replay.Status, replay.Body)
	}
	if replayed := replay.Header.Get("Idempotency-Replayed"); replayed != "true" {
		t.Errorf("replay header = %q, want Idempotency-Replayed: true", replayed)
	}
	_ = replayReq

	_, resp = f.patchMe(t, token, "", profilePatch(map[string]any{"displayName": nil}))
	if resp.Status != http.StatusOK {
		t.Fatalf("clear status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	me = decodeMe(t, resp.Body)
	if me.Data.Profile.DisplayName != nil {
		t.Errorf("displayName = %v, want null after clearing", me.Data.Profile.DisplayName)
	}
	if stored := storedProfileName(t, f, registered.Data.User.ID); stored != nil {
		t.Errorf("stored displayName = %q, want NULL after clearing", *stored)
	}

	_, resp = f.patchMe(t, token, "", profilePatch(map[string]any{"givenName": "Dante"}))
	if resp.Status != http.StatusOK {
		t.Fatalf("partial status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	me = decodeMe(t, resp.Body)
	if me.Data.Profile.GivenName == nil || *me.Data.Profile.GivenName != "Dante" {
		t.Errorf("givenName = %v, want Dante", me.Data.Profile.GivenName)
	}
	if me.Data.Profile.DisplayName != nil {
		t.Errorf("displayName = %v, want the cleared value to stay cleared", me.Data.Profile.DisplayName)
	}
}

func TestContractUsersMePatchCreatesProfileRow(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "fresh@example.com", "")

	var before int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM user_profiles")).Scan(&before); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if before != 0 {
		t.Fatalf("profiles = %d, want 0 before the first patch", before)
	}

	_, resp := f.patchMe(t, registered.Data.AccessToken, "", profilePatch(map[string]any{"familyName": "Alighieri"}))
	if resp.Status != http.StatusOK {
		t.Fatalf("patch status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}

	var after int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM user_profiles")).Scan(&after); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if after != 1 {
		t.Errorf("profiles = %d, want the patch to create the row", after)
	}
}

func TestContractUsersMePatchValidation(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "invalid@example.com", "")
	token := registered.Data.AccessToken
	longName := strings.Repeat("a", 101)

	cases := []struct {
		name    string
		body    any
		field   string
		message string
	}{
		{name: "missing profile", body: map[string]any{}, field: "profile", message: "is required"},
		{name: "empty profile", body: profilePatch(map[string]any{}), field: "profile", message: "At least one profile field must be provided"},
		{name: "empty name", body: profilePatch(map[string]any{"displayName": ""}), field: "displayName", message: "must not be empty"},
		{name: "blank name", body: profilePatch(map[string]any{"displayName": "   "}), field: "displayName", message: "must not be empty"},
		{name: "over long name", body: profilePatch(map[string]any{"givenName": longName}), field: "givenName", message: "must be at most 100 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp := f.patchMe(t, token, "", tc.body)
			if resp.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
			f.suite.AssertErrorCodeDeclared(t, "users.me.patch", "VALIDATION_FAILED")
			issues := decodeMeIssues(t, resp.Body)
			if len(issues.Errors) != 1 || issues.Errors[0].Field != tc.field || issues.Errors[0].Message != tc.message {
				t.Errorf("errors = %+v, want %s %q", issues.Errors, tc.field, tc.message)
			}
		})
	}

	t.Run("wrong type", func(t *testing.T) {
		_, resp := f.patchMe(t, token, "", profilePatch(map[string]any{"displayName": 5}))
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
	})
}

func TestContractUsersMePatchUnauthorized(t *testing.T) {
	f := newAuthFixture(t)
	body := profilePatch(map[string]any{"displayName": "Dante"})

	_, resp := f.patchMe(t, "", "", body)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
	f.suite.AssertErrorCodeDeclared(t, "users.me.patch", "UNAUTHORIZED")

	registered := f.register(t, "gone-patch@example.com", "")
	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = 'DELETED' WHERE id = ?"), registered.Data.User.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	_, resp = f.patchMe(t, registered.Data.AccessToken, "", body)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("deleted status = %d, want 401\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
}

func TestContractUsersMePatchGuardPrecedesIdempotency(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "outer-me@example.com", "")
	body := profilePatch(map[string]any{"displayName": "Dante"})

	_, resp := f.patchMe(t, "", "me-outer-key", body)
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", resp.Status)
	}

	_, resp = f.patchMe(t, registered.Data.AccessToken, "me-outer-key", body)
	if resp.Status != http.StatusOK {
		t.Fatalf("authorized status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	if replayed := resp.Header.Get("Idempotency-Replayed"); replayed != "" {
		t.Errorf("Idempotency-Replayed = %q, want empty: the unauthorized request must not prime the store", replayed)
	}
}
