package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/contracttest"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

type meDeletionEnvelope struct {
	Data struct {
		AccountDeletion *struct {
			RequestedAt  string `json:"requestedAt"`
			ScheduledFor string `json:"scheduledFor"`
		} `json:"accountDeletion"`
	} `json:"data"`
}

func (f *authFixture) deletionRequest(t *testing.T, token, idempotencyKey string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.jsonRequest(t, "/v1/me/account-deletion/request", token, idempotencyKey, map[string]any{})
}

func (f *authFixture) deletionCancel(t *testing.T, token, idempotencyKey string) (*http.Request, contracttest.Response) {
	t.Helper()
	return f.jsonRequest(t, "/v1/me/account-deletion/cancel", token, idempotencyKey, map[string]any{})
}

func decodeMeDeletion(t *testing.T, f *authFixture, token string) meDeletionEnvelope {
	t.Helper()
	_, resp := f.getMe(t, token)
	if resp.Status != http.StatusOK {
		t.Fatalf("me status = %d, want 200\nbody: %s", resp.Status, resp.Body)
	}
	var decoded meDeletionEnvelope
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	return decoded
}

func auditCount(t *testing.T, f *authFixture, action string) int {
	t.Helper()
	var count int
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT COUNT(*) FROM user_account_deletion_audits WHERE action = ?"), action).Scan(&count); err != nil {
		t.Fatalf("count audits: %v", err)
	}
	return count
}

func TestContractUsersMeAccountDeletionRequest(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "deletion@example.com", "")
	token := registered.Data.AccessToken

	req, resp := f.deletionRequest(t, token, "")
	if resp.Status != http.StatusNoContent {
		t.Fatalf("request status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, req, resp)
	contracttest.AssertRequestID(t, resp)
	if len(resp.Body) != 0 {
		t.Errorf("request body = %q, want empty", resp.Body)
	}

	me := decodeMeDeletion(t, f, token)
	if me.Data.AccountDeletion == nil {
		t.Fatal("accountDeletion = nil after the request, want the schedule")
	}
	scheduledFor, err := time.Parse(time.RFC3339Nano, me.Data.AccountDeletion.ScheduledFor)
	if err != nil {
		t.Fatalf("scheduledFor = %q: %v", me.Data.AccountDeletion.ScheduledFor, err)
	}
	if until := time.Until(scheduledFor); until <= 29*24*time.Hour || until > 30*24*time.Hour {
		t.Errorf("scheduledFor = %v, want roughly 30 days out", scheduledFor)
	}
	if me.Data.AccountDeletion.RequestedAt == "" {
		t.Error("requestedAt is empty")
	}
	if count := auditCount(t, f, "REQUESTED"); count != 1 {
		t.Errorf("REQUESTED audits = %d, want 1", count)
	}

	var actor, traceID string
	if err := f.database.SQL().QueryRowContext(t.Context(), dbtest.Rebind("SELECT actor_user_id, COALESCE(trace_id, '') FROM user_account_deletion_audits WHERE action = 'REQUESTED'")).Scan(&actor, &traceID); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if actor != registered.Data.User.ID || traceID == "" {
		t.Errorf("audit = %s/%q, want the actor and the request trace id", actor, traceID)
	}

	_, repeat := f.deletionRequest(t, token, "")
	if repeat.Status != http.StatusNoContent {
		t.Fatalf("repeat status = %d, want 204", repeat.Status)
	}
	if count := auditCount(t, f, "REQUESTED"); count != 1 {
		t.Errorf("REQUESTED audits after repeat = %d, want 1", count)
	}
	afterRepeat := decodeMeDeletion(t, f, token)
	if afterRepeat.Data.AccountDeletion == nil || afterRepeat.Data.AccountDeletion.ScheduledFor != me.Data.AccountDeletion.ScheduledFor {
		t.Error("scheduledFor changed on a repeat request")
	}

	_, replay := f.deletionRequest(t, token, "deletion-key-1")
	if replay.Status != http.StatusNoContent {
		t.Fatalf("keyed status = %d, want 204", replay.Status)
	}
	_, replayAgain := f.deletionRequest(t, token, "deletion-key-1")
	if replayed := replayAgain.Header.Get("Idempotency-Replayed"); replayed != "true" {
		t.Errorf("Idempotency-Replayed = %q, want true", replayed)
	}
	if count := auditCount(t, f, "REQUESTED"); count != 1 {
		t.Errorf("REQUESTED audits after replay = %d, want 1", count)
	}
}

func TestContractUsersMeAccountDeletionCancel(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "cancel@example.com", "")
	token := registered.Data.AccessToken
	if _, resp := f.deletionRequest(t, token, ""); resp.Status != http.StatusNoContent {
		t.Fatalf("request status = %d, want 204", resp.Status)
	}

	cancelReq, resp := f.deletionCancel(t, token, "cancel-key-1")
	if resp.Status != http.StatusNoContent {
		t.Fatalf("cancel status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
	f.suite.ValidateResponse(t, cancelReq, resp)
	if count := auditCount(t, f, "CANCELED"); count != 1 {
		t.Errorf("CANCELED audits = %d, want 1", count)
	}
	me := decodeMeDeletion(t, f, token)
	if me.Data.AccountDeletion != nil {
		t.Errorf("accountDeletion = %+v after cancel, want null", me.Data.AccountDeletion)
	}

	_, repeat := f.deletionCancel(t, token, "")
	if repeat.Status != http.StatusNoContent {
		t.Fatalf("repeat cancel status = %d, want 204", repeat.Status)
	}
	if count := auditCount(t, f, "CANCELED"); count != 1 {
		t.Errorf("CANCELED audits after repeat = %d, want 1", count)
	}
}

func TestContractUsersMeAccountDeletionLastAdmin(t *testing.T) {
	f := newAuthFixture(t)
	registered := f.register(t, "admin@example.com", "")
	token := registered.Data.AccessToken
	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET role = 'ADMIN' WHERE id = ?"), registered.Data.User.ID); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	_, resp := f.deletionRequest(t, token, "")
	if resp.Status != http.StatusConflict {
		t.Fatalf("last admin status = %d, want 409\nbody: %s", resp.Status, resp.Body)
	}
	contracttest.AssertProblem(t, resp, "USERS_CANNOT_DELETE_LAST_ADMIN")
	f.suite.AssertErrorCodeDeclared(t, "users.me.accountDeletion.request", "USERS_CANNOT_DELETE_LAST_ADMIN")
	if me := decodeMeDeletion(t, f, token); me.Data.AccountDeletion != nil {
		t.Error("deletion scheduled for the last admin")
	}

	second := f.register(t, "admin2@example.com", "")
	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET role = 'ADMIN' WHERE id = ?"), second.Data.User.ID); err != nil {
		t.Fatalf("promote second admin: %v", err)
	}
	if _, resp := f.deletionRequest(t, token, ""); resp.Status != http.StatusNoContent {
		t.Fatalf("admin with a peer status = %d, want 204\nbody: %s", resp.Status, resp.Body)
	}
}

func TestContractUsersMeAccountDeletionUnauthorized(t *testing.T) {
	f := newAuthFixture(t)

	_, request := f.deletionRequest(t, "", "")
	if request.Status != http.StatusUnauthorized {
		t.Fatalf("request without a token = %d, want 401", request.Status)
	}
	contracttest.AssertProblem(t, request, "UNAUTHORIZED")
	f.suite.AssertErrorCodeDeclared(t, "users.me.accountDeletion.request", "UNAUTHORIZED")
	_, cancel := f.deletionCancel(t, "", "")
	if cancel.Status != http.StatusUnauthorized {
		t.Fatalf("cancel without a token = %d, want 401", cancel.Status)
	}

	leaving := f.register(t, "leaving-deletion@example.com", "")
	if _, err := f.database.SQL().ExecContext(t.Context(), dbtest.Rebind("UPDATE users SET status = 'DELETED' WHERE id = ?"), leaving.Data.User.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	_, deletedRequest := f.deletionRequest(t, leaving.Data.AccessToken, "")
	if deletedRequest.Status != http.StatusUnauthorized {
		t.Fatalf("deleted request = %d, want 401", deletedRequest.Status)
	}
	_, deletedCancel := f.deletionCancel(t, leaving.Data.AccessToken, "")
	if deletedCancel.Status != http.StatusUnauthorized {
		t.Fatalf("deleted cancel = %d, want 401", deletedCancel.Status)
	}
}
