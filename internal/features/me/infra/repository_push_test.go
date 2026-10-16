//go:build !pg

package infra_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/features/me/domain"
	"github.com/fikrilal/go-core-kit/internal/features/me/infra"
	"github.com/fikrilal/go-core-kit/internal/features/me/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

const pushTestStamp = "2026-01-01T00:00:00.000000000Z"

func TestSessionForPushAndClear(t *testing.T) {
	database := db.MustOpen(filepath.Join(t.TempDir(), "push.db"))
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.SQL().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %s: %v", query, err)
		}
	}
	exec("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)", "user-1", "user@example.com", pushTestStamp, pushTestStamp)
	exec(`INSERT INTO sessions (id, user_id, push_platform, push_token, last_seen_at, expires_at, created_at, updated_at)
	      VALUES (?, ?, 'ANDROID', ?, ?, ?, ?, ?)`, "session-1", "user-1", "device-token-1", pushTestStamp, "2099-01-01T00:00:00.000000000Z", pushTestStamp, pushTestStamp)

	repository := infra.NewRepository(database)
	session, found, err := repository.SessionForPush(ctx, "session-1")
	if err != nil || !found {
		t.Fatalf("SessionForPush() = %+v/%t/%v", session, found, err)
	}
	if session.PushToken != "device-token-1" || session.Revoked || session.UserStatus != "ACTIVE" || session.ExpiresAt.IsZero() {
		t.Errorf("session = %+v, want the stored token and account state", session)
	}

	// A stale token must not be cleared.
	if err := repository.ClearSessionPushTokenIfMatches(ctx, "session-1", "other-token", time.Now()); err != nil {
		t.Fatalf("ClearSessionPushTokenIfMatches(stale) error = %v", err)
	}
	var token *string
	if err := database.SQL().QueryRowContext(ctx, "SELECT push_token FROM sessions WHERE id = 'session-1'").Scan(&token); err != nil {
		t.Fatalf("read token: %v", err)
	}
	if token == nil || *token != "device-token-1" {
		t.Fatalf("token = %v, want the concurrent registration preserved", token)
	}

	if err := repository.ClearSessionPushTokenIfMatches(ctx, "session-1", "device-token-1", time.Now()); err != nil {
		t.Fatalf("ClearSessionPushTokenIfMatches() error = %v", err)
	}
	if err := database.SQL().QueryRowContext(ctx, "SELECT push_token FROM sessions WHERE id = 'session-1'").Scan(&token); err != nil && !errors.Is(err, db.ErrNoRows) {
		t.Fatalf("read token: %v", err)
	}
	if token != nil {
		t.Errorf("token = %q, want NULL after the matching clear", *token)
	}

	if _, found, err := repository.SessionForPush(ctx, "missing"); err != nil || found {
		t.Fatalf("SessionForPush(missing) = %t/%v, want not found", found, err)
	}
}

func TestFinalizeDeletionScrubsAndCascades(t *testing.T) {
	database := db.MustOpen(filepath.Join(t.TempDir(), "finalize.db"))
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.SQL().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %s: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, email, role, status, email_verified_at, created_at, updated_at, deletion_requested_at, deletion_scheduled_for, deletion_requested_session_id, deletion_requested_trace_id)
	      VALUES ('user-1', 'user@example.com', 'USER', 'ACTIVE', ?, ?, ?, ?, ?, 'session-1', 'trace-1')`,
		pushTestStamp, pushTestStamp, pushTestStamp, pushTestStamp, pushTestStamp)
	exec("INSERT INTO user_profiles (user_id, display_name, given_name, family_name, created_at, updated_at) VALUES ('user-1', 'Dee', 'Dee', 'Ee', ?, ?)", pushTestStamp, pushTestStamp)
	exec("INSERT INTO password_credentials (user_id, password_hash, created_at, updated_at) VALUES ('user-1', 'hash', ?, ?)", pushTestStamp, pushTestStamp)
	exec("INSERT INTO external_identities (id, user_id, provider, subject, created_at, updated_at) VALUES ('ext-1', 'user-1', 'GOOGLE', 'sub-1', ?, ?)", pushTestStamp, pushTestStamp)
	exec(`INSERT INTO email_verification_tokens (id, user_id, token_hash, expires_at, created_at, updated_at)
	      VALUES ('evt-1', 'user-1', 'hash', '2099-01-01T00:00:00.000000000Z', ?, ?)`, pushTestStamp, pushTestStamp)
	exec(`INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at, created_at, updated_at)
	      VALUES ('prt-1', 'user-1', 'hash', '2099-01-01T00:00:00.000000Z', ?, ?)`, pushTestStamp, pushTestStamp)
	exec(`INSERT INTO sessions (id, user_id, last_seen_at, created_at, updated_at, expires_at) VALUES ('session-1', 'user-1', ?, ?, ?, '2099-01-01T00:00:00.000000000Z')`,
		pushTestStamp, pushTestStamp, pushTestStamp)

	repository := infra.NewRepository(database)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if _, err := repository.FinalizeDeletion(ctx, ports.FinalizeDeletionInput{
		UserID:         "user-1",
		ScrubbedEmail:  "deleted+user-1@example.invalid",
		ActorSessionID: "session-1",
		TraceID:        "trace-1",
		Now:            now,
	}); err != nil {
		t.Fatalf("FinalizeDeletion() error = %v", err)
	}

	var email, role, status string
	var verified, scheduled *string
	var deletedAt string
	if err := database.SQL().QueryRowContext(ctx,
		"SELECT email, role, status, email_verified_at, deletion_scheduled_for, deleted_at FROM users WHERE id = 'user-1'").
		Scan(&email, &role, &status, &verified, &scheduled, &deletedAt); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if email != "deleted+user-1@example.invalid" || role != "USER" || status != "DELETED" {
		t.Errorf("user = %s/%s/%s, want the scrubbed de-identified row", email, role, status)
	}
	if verified != nil || scheduled != nil || deletedAt == "" {
		t.Errorf("columns = %v/%v/%q, want the cleared state with deleted_at set", verified, scheduled, deletedAt)
	}
	assertFinalizedRows(t, database)

	var action, sessionID, traceID string
	if err := database.SQL().QueryRowContext(ctx,
		"SELECT action, actor_session_id, trace_id FROM user_account_deletion_audits WHERE target_user_id = 'user-1'").
		Scan(&action, &sessionID, &traceID); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if action != "FINALIZED" || sessionID != "session-1" || traceID != "trace-1" {
		t.Errorf("audit = %s/%s/%s, want the requester's session and trace", action, sessionID, traceID)
	}
}

func TestRequestAccountDeletionRoundTrip(t *testing.T) {
	database := db.MustOpen(filepath.Join(t.TempDir(), "request.db"))
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	ctx := t.Context()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	repository := infra.NewRepository(database)
	if _, err := database.SQL().ExecContext(ctx,
		"INSERT INTO users (id, email, created_at, updated_at) VALUES ('user-1', 'user@example.com', ?, ?)",
		pushTestStamp, pushTestStamp); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	kind, _, err := repository.RequestAccountDeletion(ctx, ports.RequestAccountDeletionInput{
		UserID: "user-1", SessionID: "session-1", TraceID: "trace-1", Now: now, ScheduledFor: now.Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("RequestAccountDeletion() error = %v", err)
	}
	if kind != domain.DeletionOK {
		t.Fatalf("kind = %q, want ok", kind)
	}
	cancelled, err := repository.CancelAccountDeletion(ctx, ports.CancelAccountDeletionInput{
		UserID: "user-1", SessionID: "session-1", TraceID: "trace-1", Now: now,
	})
	if err != nil {
		t.Fatalf("CancelAccountDeletion() error = %v", err)
	}
	if cancelled != domain.DeletionOK {
		t.Fatalf("kind = %q, want ok", cancelled)
	}
}

// assertFinalizedRows checks the profile, credential, identity, and token rows.
func assertFinalizedRows(t *testing.T, database *db.Database) {
	t.Helper()
	ctx := context.Background()
	for _, check := range []struct {
		table string
		where string
		want  int
	}{
		{"user_profiles", "user_id = 'user-1' AND display_name IS NULL AND given_name IS NULL AND family_name IS NULL", 1},
		{"password_credentials", "user_id = 'user-1'", 0},
		{"external_identities", "user_id = 'user-1'", 0},
		{"sessions", "user_id = 'user-1'", 0},
		{"email_verification_tokens", "user_id = 'user-1'", 0},
		{"password_reset_tokens", "user_id = 'user-1'", 0},
	} {
		var count int
		if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+check.table+" WHERE "+check.where).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", check.table, err)
		}
		if count != check.want {
			t.Errorf("%s rows = %d, want %d (%s)", check.table, count, check.want, check.where)
		}
	}
}

func TestFinalizeDeletionLastAdminGuard(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		status     string
		otherAdmin bool
		wantKind   domain.FinalizeKind
	}{
		{name: "lone active admin is blocked", role: "ADMIN", status: "ACTIVE", wantKind: domain.FinalizeBlockedLastAdmin},
		{name: "suspended admin finalizes", role: "ADMIN", status: "SUSPENDED", otherAdmin: true, wantKind: domain.FinalizeDone},
		{name: "admin with a peer finalizes", role: "ADMIN", status: "ACTIVE", otherAdmin: true, wantKind: domain.FinalizeDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database := db.MustOpen(filepath.Join(t.TempDir(), "guard.db"))
			t.Cleanup(func() { _ = database.Close() })
			if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
				t.Fatalf("Migrate() error = %v", err)
			}
			ctx := t.Context()
			seedGuardUser(t, database, tt.role, tt.status, tt.otherAdmin)
			repository := infra.NewRepository(database)
			kind, err := repository.FinalizeDeletion(ctx, ports.FinalizeDeletionInput{
				UserID:         "user-1",
				ScrubbedEmail:  "deleted+user-1@example.invalid",
				ActorSessionID: "session-1",
				TraceID:        "trace-1",
				Now:            time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("FinalizeDeletion() error = %v", err)
			}
			if kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", kind, tt.wantKind)
			}
			assertGuardOutcome(t, database, tt.wantKind)
		})
	}
}

// seedGuardUser inserts the target admin and, optionally, a peer admin.
func seedGuardUser(t *testing.T, database *db.Database, role, status string, otherAdmin bool) {
	t.Helper()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.SQL().ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec %s: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, email, role, status, created_at, updated_at, deletion_requested_at, deletion_scheduled_for, deletion_requested_session_id, deletion_requested_trace_id)
	      VALUES ('user-1', 'admin@example.com', ?, ?, ?, ?, ?, ?, 'session-1', 'trace-1')`,
		role, status, pushTestStamp, pushTestStamp, pushTestStamp, pushTestStamp, pushTestStamp)
	if otherAdmin {
		exec(`INSERT INTO users (id, email, role, status, created_at, updated_at)
		      VALUES ('user-2', 'peer@example.com', 'ADMIN', 'ACTIVE', ?, ?)`, pushTestStamp, pushTestStamp)
	}
}

// assertGuardOutcome checks the target row and the audit the transaction wrote.
func assertGuardOutcome(t *testing.T, database *db.Database, wantKind domain.FinalizeKind) {
	t.Helper()
	ctx := t.Context()
	var status, email string
	if err := database.SQL().QueryRowContext(ctx, "SELECT status, email FROM users WHERE id = 'user-1'").Scan(&status, &email); err != nil {
		t.Fatalf("read user: %v", err)
	}
	var auditAction *string
	_ = database.SQL().QueryRowContext(ctx, "SELECT action FROM user_account_deletion_audits WHERE target_user_id = 'user-1'").Scan(&auditAction)
	if wantKind == domain.FinalizeBlockedLastAdmin {
		if status != "ACTIVE" || email != "admin@example.com" {
			t.Errorf("user = %s/%s, want the account kept", status, email)
		}
		if auditAction == nil || *auditAction != "FINALIZE_BLOCKED_LAST_ADMIN" {
			t.Errorf("audit = %v, want the blocked row in the same transaction", auditAction)
		}
		return
	}
	if status != "DELETED" || !strings.HasPrefix(email, "deleted+user-1@") {
		t.Errorf("user = %s/%s, want the de-identified row", status, email)
	}
	if auditAction == nil || *auditAction != "FINALIZED" {
		t.Errorf("audit = %v, want the finalize row", auditAction)
	}
}
