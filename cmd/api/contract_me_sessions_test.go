package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

type meSessionsEnvelope struct {
	Data []struct {
		ID         string  `json:"id"`
		DeviceID   *string `json:"deviceId"`
		DeviceName *string `json:"deviceName"`
		IP         *string `json:"ip"`
		UserAgent  *string `json:"userAgent"`
		CreatedAt  string  `json:"createdAt"`
		LastSeenAt string  `json:"lastSeenAt"`
		ExpiresAt  string  `json:"expiresAt"`
		RevokedAt  *string `json:"revokedAt"`
		Current    bool    `json:"current"`
		Status     string  `json:"status"`
	} `json:"data"`
	Meta struct {
		NextCursor *string `json:"nextCursor"`
		Limit      float64 `json:"limit"`
		HasMore    bool    `json:"hasMore"`
	} `json:"meta"`
}

func decodeMeSessions(t *testing.T, body []byte) meSessionsEnvelope {
	t.Helper()
	var decoded meSessionsEnvelope
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode sessions response: %v", err)
	}
	return decoded
}

func (f *authFixture) listSessions(t *testing.T, token, query string) (*http.Request, contracttest.Response) {
	t.Helper()
	url := f.baseURL + "/v1/me/sessions"
	if query != "" {
		url += "?" + query
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, contracttest.Do(t, f.client, req)
}

func (f *authFixture) revokeSession(t *testing.T, token, sessionID string) (*http.Request, contracttest.Response) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		f.baseURL+"/v1/me/sessions/"+sessionID+"/revoke", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, contracttest.Do(t, f.client, req)
}

func TestContractUsersMeSessionsList(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "sessions@example.com", "device-a")
	second := f.login(t, "sessions@example.com", testPassword, "device-b")
	if second.Status != http.StatusOK {
		t.Fatalf("login status = %d, want 200", second.Status)
	}
	current := decodeAuthResponse(t, second.Body)

	req, resp := f.listSessions(t, current.Data.AccessToken, "")
	if resp.Status != http.StatusOK {
		t.Fatalf("list status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)

	page := decodeMeSessions(t, resp.Body)
	if page.Meta.Limit != 25 || page.Meta.HasMore || page.Meta.NextCursor != nil {
		t.Errorf("meta = %+v, want the default single page", page.Meta)
	}
	if len(page.Data) != 2 {
		t.Fatalf("sessions = %d, want 2", len(page.Data))
	}
	newest := page.Data[0]
	oldest := page.Data[1]
	if newest.DeviceID == nil || *newest.DeviceID != "device-b" || !newest.Current {
		t.Errorf("first item = %+v, want the newer current session", newest)
	}
	if oldest.DeviceID == nil || *oldest.DeviceID != "device-a" || oldest.Current {
		t.Errorf("second item = %+v, want the older session", oldest)
	}
	for _, item := range page.Data {
		if item.Status != "active" || item.RevokedAt != nil {
			t.Errorf("item %s = status %q revokedAt %v, want an active session", item.ID, item.Status, item.RevokedAt)
		}
		if item.ID == "" || item.ExpiresAt == "" || item.LastSeenAt == "" {
			t.Errorf("item = %+v, want the stored fields", item)
		}
	}
}

func TestContractUsersMeSessionsPagination(t *testing.T) {
	f := newAuthFixture(t)
	f.register(t, "paging@example.com", "device-a")
	second := f.login(t, "paging@example.com", testPassword, "device-b")
	current := decodeAuthResponse(t, second.Body)
	token := current.Data.AccessToken

	firstReq, firstResp := f.listSessions(t, token, "limit=1")
	if firstResp.Status != http.StatusOK {
		t.Fatalf("first page status = %d, want 200\nbody: %s", firstResp.Status, firstResp.Body)
	}
	first := decodeMeSessions(t, firstResp.Body)
	if len(first.Data) != 1 || !first.Meta.HasMore || first.Meta.NextCursor == nil {
		t.Fatalf("first page = %+v, want one item with a cursor", first)
	}

	_, secondResp := f.listSessions(t, token, "limit=1&cursor="+*first.Meta.NextCursor)
	if secondResp.Status != http.StatusOK {
		t.Fatalf("second page status = %d, want 200\nbody: %s", secondResp.Status, secondResp.Body)
	}
	next := decodeMeSessions(t, secondResp.Body)
	if len(next.Data) != 1 || next.Meta.HasMore || next.Meta.NextCursor != nil {
		t.Fatalf("second page = %+v, want the final item", next)
	}
	if next.Data[0].ID == first.Data[0].ID {
		t.Error("pagination returned the same session twice")
	}

	_, sortedResp := f.listSessions(t, token, "sort=id")
	if sortedResp.Status != http.StatusOK {
		t.Fatalf("sorted status = %d, want 200\nbody: %s", sortedResp.Status, sortedResp.Body)
	}
	sorted := decodeMeSessions(t, sortedResp.Body)
	if len(sorted.Data) != 2 || sorted.Data[0].ID >= sorted.Data[1].ID {
		t.Errorf("sorted ids = %s, %s; want ascending id order", sorted.Data[0].ID, sorted.Data[1].ID)
	}
	_ = firstReq

	t.Run("cursor for another sort", func(t *testing.T) {
		_, resp := f.listSessions(t, token, "sort=-id&cursor="+*first.Meta.NextCursor)
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
	})

	t.Run("malformed cursor", func(t *testing.T) {
		_, resp := f.listSessions(t, token, "cursor=not-a-cursor")
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		issues := decodeMeIssues(t, resp.Body)
		if len(issues.Errors) != 1 || issues.Errors[0].Field != "cursor" {
			t.Errorf("errors = %+v, want the cursor issue", issues.Errors)
		}
	})
}

func TestContractUsersMeSessionsListValidation(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "list-invalid@example.com", "")
	token := registered.Data.AccessToken

	cases := []struct {
		name  string
		query string
		field string
	}{
		{name: "limit zero", query: "limit=0", field: "limit"},
		{name: "limit over max", query: "limit=101", field: "limit"},
		{name: "limit not a number", query: "limit=abc", field: "limit"},
		{name: "unknown sort", query: "sort=userId", field: "sort"},
		{name: "too many sorts", query: "sort=createdAt,id,createdAt,id", field: "sort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, resp := f.listSessions(t, token, tc.query)
			if resp.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
			}
			contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
			f.suite.AssertErrorCodeDeclared(t, "users.me.sessions.list", "VALIDATION_FAILED")
			issues := decodeMeIssues(t, resp.Body)
			if len(issues.Errors) != 1 || issues.Errors[0].Field != tc.field {
				t.Errorf("errors = %+v, want the %s issue", issues.Errors, tc.field)
			}
		})
	}

	t.Run("missing token", func(t *testing.T) {
		_, resp := f.listSessions(t, "", "")
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
	})
}

func TestContractUsersMeSessionsRevoke(t *testing.T) {
	f := newAuthFixture(t)
	first := f.register(t, "revoke@example.com", "device-a")
	second := f.login(t, "revoke@example.com", testPassword, "device-b")
	current := decodeAuthResponse(t, second.Body)

	_, listResp := f.listSessions(t, current.Data.AccessToken, "")
	page := decodeMeSessions(t, listResp.Body)
	var otherID string
	for _, item := range page.Data {
		if !item.Current {
			otherID = item.ID
		}
	}
	if otherID == "" {
		t.Fatal("no non-current session found to revoke")
	}

	req, resp := f.revokeSession(t, current.Data.AccessToken, otherID)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)
	if len(resp.Body) != 0 {
		t.Errorf("revoke body = %q, want empty", resp.Body)
	}

	_, revokedRefresh := f.refresh(t, first.Data.RefreshToken)
	if revokedRefresh.Status != http.StatusUnauthorized {
		t.Fatalf("revoked refresh status = %d, want 401\nbody: %s", revokedRefresh.Status, revokedRefresh.Body)
	}
	contracttest.AssertProblem(t, revokedRefresh, "AUTH_SESSION_REVOKED")

	_, listResp = f.listSessions(t, current.Data.AccessToken, "")
	page = decodeMeSessions(t, listResp.Body)
	foundRevoked := false
	for _, item := range page.Data {
		if item.ID == otherID {
			foundRevoked = item.Status == "revoked" && item.RevokedAt != nil
		}
	}
	if !foundRevoked {
		t.Error("revoked session not reported as revoked with revokedAt")
	}

	if _, resp := f.revokeSession(t, current.Data.AccessToken, otherID); resp.Status != http.StatusNoContent {
		t.Fatalf("repeat revoke status = %d, want 204 (idempotent)", resp.Status)
	}
	if resp := f.login(t, "revoke@example.com", testPassword, "device-b"); resp.Status != http.StatusOK {
		t.Fatalf("current session login status = %d, want 200", resp.Status)
	}
}

func TestContractUsersMeSessionsRevokeErrors(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "revoke-errors@example.com", "")
	token := registered.Data.AccessToken

	t.Run("invalid uuid", func(t *testing.T) {
		_, resp := f.revokeSession(t, token, "not-a-uuid")
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "VALIDATION_FAILED")
		f.suite.AssertErrorCodeDeclared(t, "users.me.sessions.revoke", "VALIDATION_FAILED")
	})

	t.Run("unknown session", func(t *testing.T) {
		_, resp := f.revokeSession(t, token, "0f8b9d3e-1a2b-4c3d-8e4f-5a6b7c8d9e0f")
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "NOT_FOUND")
		f.suite.AssertErrorCodeDeclared(t, "users.me.sessions.revoke", "NOT_FOUND")
	})

	t.Run("foreign session", func(t *testing.T) {
		_, listResp := f.listSessions(t, token, "")
		page := decodeMeSessions(t, listResp.Body)
		if len(page.Data) == 0 {
			t.Fatal("no session to target")
		}
		foreignID := page.Data[0].ID

		other := f.register(t, "foreign@example.com", "")
		_, resp := f.revokeSession(t, other.Data.AccessToken, foreignID)
		if resp.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 for another user's session\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "NOT_FOUND")
	})

	t.Run("missing token", func(t *testing.T) {
		_, resp := f.revokeSession(t, "", "0f8b9d3e-1a2b-4c3d-8e4f-5a6b7c8d9e0f")
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
	})

	t.Run("deleted user", func(t *testing.T) {
		leaving := f.register(t, "leaving@example.com", "")
		if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = 'DELETED' WHERE id = ?"), leaving.Data.User.ID); err != nil {
			t.Fatalf("delete user: %v", err)
		}
		_, resp := f.revokeSession(t, leaving.Data.AccessToken, "0f8b9d3e-1a2b-4c3d-8e4f-5a6b7c8d9e0f")
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 before the 404\nbody: %s", resp.Status, resp.Body)
		}
		contracttest.AssertProblem(t, resp, "UNAUTHORIZED")
	})
}
