package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

// TestDBMoveRoundTrip exports a seeded database and imports it into a second one,
// which is the lane's own proof that the snapshot format survives the trip. The
// cross-engine move runs the same code path twice, once per compiled lane.
func TestDBMoveRoundTrip(t *testing.T) {
	source := dbtest.Open(t)
	ctx := t.Context()
	const stamp = "2026-01-01T00:00:00.000000000Z"
	if _, err := source.SQL().ExecContext(ctx, "INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"user-1", "move@example.com", stamp, stamp); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := source.SQL().ExecContext(ctx, "INSERT INTO password_credentials (user_id, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"user-1", "hash-1", stamp, stamp); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	snapshot := filepath.Join(t.TempDir(), "snapshot.jsonl")
	if err := runDBExport(ctx, source, snapshot); err != nil {
		t.Fatalf("runDBExport() error = %v", err)
	}

	target := dbtest.Open(t)
	if err := runDBImport(ctx, target, snapshot, false, false); err != nil {
		t.Fatalf("runDBImport() error = %v", err)
	}
	var email, hash string
	if err := target.SQL().QueryRowContext(ctx, "SELECT u.email, c.password_hash FROM users u JOIN password_credentials c ON c.user_id = u.id").Scan(&email, &hash); err != nil {
		t.Fatalf("read imported rows: %v", err)
	}
	if email != "move@example.com" || hash != "hash-1" {
		t.Errorf("imported = %s/%s, want the seeded pair", email, hash)
	}

	// A second import into the same target has to be refused without --replace,
	// and a dry run must leave the target alone.
	err := runDBImport(ctx, target, snapshot, false, false)
	if err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("runDBImport(non-empty target) error = %v, want the --replace refusal", err)
	}
	if err := runDBImport(ctx, target, snapshot, true, true); err != nil {
		t.Fatalf("runDBImport(dry-run) error = %v", err)
	}
	if err := runDBImport(ctx, target, snapshot, true, false); err != nil {
		t.Fatalf("runDBImport(replace) error = %v", err)
	}
	var users int
	if err := target.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Errorf("users = %d, want the replace to leave exactly one row", users)
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Errorf("snapshot missing: %v", err)
	}
}
