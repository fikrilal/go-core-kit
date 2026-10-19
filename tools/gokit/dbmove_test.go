package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/dbtest"
)

// TestDBMoveRoundTrip exports a seeded database and imports it into a second one,
// which is the lane's own proof that the snapshot format survives the trip. The
// cross-engine move runs the same code path twice, once per compiled lane.
func TestDBMoveRoundTrip(t *testing.T) {
	source := dbtest.Open(t)
	ctx := t.Context()
	const stamp = "2026-01-01T00:00:00.000000000Z"
	if _, err := source.SQL().ExecContext(ctx, dbtest.Rebind("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)"),
		"user-1", "move@example.com", stamp, stamp); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := source.SQL().ExecContext(ctx, dbtest.Rebind("INSERT INTO password_credentials (user_id, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)"),
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

// TestDBMoveRespectsForeignKeys moves the two relationships whose ordering is not
// alphabetical: a profile image attached to a profile, and a rotated refresh token
// that points at the token it replaced. Both are ordinary states of this kit, and
// both used to fail the import.
func TestDBMoveRespectsForeignKeys(t *testing.T) {
	ctx := t.Context()
	const stamp = "2026-01-01T00:00:00.000000000Z"
	source := dbtest.Open(t)
	exec := func(database *db.Database, query string, args ...any) {
		t.Helper()
		if _, err := database.SQL().ExecContext(ctx, dbtest.Rebind(query), args...); err != nil {
			t.Fatalf("exec %s: %v", query, err)
		}
	}
	exec(source, "INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)", "user-1", "fk@example.com", stamp, stamp)
	exec(source, `INSERT INTO stored_files (id, owner_user_id, bucket, object_key, content_type, size_bytes, status, purpose, created_at, updated_at)
	              VALUES (?, ?, 'bucket', 'key', 'image/png', 10, 'ACTIVE', 'PROFILE_IMAGE', ?, ?)`, "file-1", "user-1", stamp, stamp)
	exec(source, `INSERT INTO user_profiles (user_id, profile_image_file_id, created_at, updated_at) VALUES (?, ?, ?, ?)`, "user-1", "file-1", stamp, stamp)
	exec(source, `INSERT INTO sessions (id, user_id, last_seen_at, created_at, updated_at, expires_at)
	              VALUES (?, ?, ?, ?, ?, ?)`, "session-1", "user-1", stamp, stamp, stamp, "2099-01-01T00:00:00.000000000Z")
	exec(source, `INSERT INTO refresh_tokens (id, token_hash, session_id, created_at, updated_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`, "token-1", "hash-1", "session-1", stamp, stamp, "2099-01-01T00:00:00.000000000Z")
	exec(source, `INSERT INTO refresh_tokens (id, token_hash, session_id, replaced_by_id, created_at, updated_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, "token-2", "hash-2", "session-1", "token-1", stamp, stamp, "2099-01-01T00:00:00.000000000Z")

	snapshot := filepath.Join(t.TempDir(), "fk.jsonl")
	if err := runDBExport(ctx, source, snapshot); err != nil {
		t.Fatalf("runDBExport() error = %v", err)
	}
	target := dbtest.Open(t)
	if err := runDBImport(ctx, target, snapshot, false, false); err != nil {
		t.Fatalf("runDBImport() error = %v", err)
	}
	var linked, replaced string
	if err := target.SQL().QueryRowContext(ctx, "SELECT profile_image_file_id FROM user_profiles WHERE user_id = 'user-1'").Scan(&linked); err != nil {
		t.Fatalf("read profile link: %v", err)
	}
	if linked != "file-1" {
		t.Errorf("profile link = %q, want file-1", linked)
	}
	if err := target.SQL().QueryRowContext(ctx, "SELECT replaced_by_id FROM refresh_tokens WHERE id = 'token-2'").Scan(&replaced); err != nil {
		t.Fatalf("read replaced_by_id: %v", err)
	}
	if replaced != "token-1" {
		t.Errorf("replaced_by_id = %q, want the link restored", replaced)
	}
}

// TestDBMoveImportIsAtomic proves a failure anywhere leaves the target untouched,
// which is what makes --replace safe: the deletes and the inserts share a transaction.
func TestDBMoveImportIsAtomic(t *testing.T) {
	ctx := t.Context()
	const stamp = "2026-01-01T00:00:00.000000000Z"
	source := dbtest.Open(t)
	if _, err := source.SQL().ExecContext(ctx, dbtest.Rebind("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)"),
		"user-1", "atomic@example.com", stamp, stamp); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	snapshot := filepath.Join(t.TempDir(), "atomic.jsonl")
	if err := runDBExport(ctx, source, snapshot); err != nil {
		t.Fatalf("runDBExport() error = %v", err)
	}

	// A snapshot whose user row has no email cannot be inserted: the column is NOT
	// NULL. With --replace the import has already deleted the target's rows by then,
	// so a failure that is not atomic shows up as a target left empty.
	//nolint:gosec // G304: the test reads the snapshot it just wrote.
	raw, err := os.ReadFile(snapshot)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	corrupted := filepath.Join(t.TempDir(), "corrupted.jsonl")
	body := strings.Replace(string(raw), `"atomic@example.com"`, "null", 1)
	//nolint:gosec // G703: the path is this test's own temp file.
	if err := os.WriteFile(corrupted, []byte(body), 0o600); err != nil {
		t.Fatalf("write corrupted snapshot: %v", err)
	}

	target := dbtest.Open(t)
	if _, err := target.SQL().ExecContext(ctx, dbtest.Rebind("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)"),
		"existing", "existing@example.com", stamp, stamp); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := runDBImport(ctx, target, corrupted, true, false); err == nil {
		t.Fatal("runDBImport(corrupted) error = nil, want the missing email to fail")
	}
	var users int
	if err := target.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = 'existing'").Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Errorf("after a failed --replace the target holds %d users, want its original row back", users)
	}
}
