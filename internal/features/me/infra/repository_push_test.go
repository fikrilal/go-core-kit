//go:build !pg

package infra_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/features/me/infra"
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
