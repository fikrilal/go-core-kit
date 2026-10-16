//go:build !pg

package db_test

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

const testNow = "2026-01-01T00:00:00.000000000Z"

func TestMigrateCreatesAuthSchema(t *testing.T) {
	database := testDatabase(t)
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	for _, table := range []string{"password_credentials", "sessions", "refresh_tokens", "user_profiles", "external_identities"} {
		if !tableExists(t, database, table) {
			t.Errorf("table %s missing", table)
		}
	}
	userColumns := tableColumns(t, database, "users")
	for _, column := range []string{"email_verified_at", "role", "status", "suspended_at", "suspended_reason"} {
		if !slices.Contains(userColumns, column) {
			t.Errorf("users missing column %s (have %v)", column, userColumns)
		}
	}
	indexes := []string{
		"sessions_user_id_idx",
		"sessions_device_id_idx",
		"sessions_revoked_at_idx",
		"sessions_push_token_revoked_at_idx",
		"refresh_tokens_session_id_idx",
		"refresh_tokens_revoked_at_idx",
	}
	for _, index := range indexes {
		if !indexExists(t, database, index) {
			t.Errorf("index %s missing", index)
		}
	}
}

func TestAuthSchemaEnforcesConstraints(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()
	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	exec := func(query string, args ...any) error {
		if _, err := database.SQL().ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("exec: %w", err)
		}
		return nil
	}
	if err := exec("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)", "u1", "a@b.co", testNow, testNow); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := exec("UPDATE users SET role = 'NOPE' WHERE id = ?", "u1"); err == nil {
		t.Error("role CHECK not enforced")
	}
	if err := exec("UPDATE users SET status = 'NOPE' WHERE id = ?", "u1"); err == nil {
		t.Error("status CHECK not enforced")
	}
	if err := exec("INSERT INTO sessions (id, user_id, push_platform, last_seen_at, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"s0", "u1", "NOPE", testNow, testNow, testNow, testNow); err == nil {
		t.Error("push_platform CHECK not enforced")
	}
	if err := exec("INSERT INTO sessions (id, user_id, last_seen_at, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"s1", "u1", testNow, testNow, testNow, testNow); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := exec("INSERT INTO sessions (id, user_id, active_key, last_seen_at, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"s2", "u1", "u1:d1", testNow, testNow, testNow, testNow); err != nil {
		t.Fatalf("insert session with active key: %v", err)
	}
	if err := exec("INSERT INTO sessions (id, user_id, active_key, last_seen_at, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"s3", "u1", "u1:d1", testNow, testNow, testNow, testNow); err == nil {
		t.Error("active_key UNIQUE not enforced")
	}
	if err := exec("INSERT INTO refresh_tokens (id, token_hash, expires_at, session_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"t1", "hash-1", testNow, "missing-session", testNow, testNow); err == nil {
		t.Error("refresh token session FK not enforced")
	}
	if err := exec("INSERT INTO refresh_tokens (id, token_hash, expires_at, session_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"t1", "hash-1", testNow, "s1", testNow, testNow); err != nil {
		t.Fatalf("insert refresh token: %v", err)
	}
	insertOidcFixtures(t, exec)

	if err := exec("DELETE FROM users WHERE id = ?", "u1"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	for _, table := range []string{"password_credentials", "sessions", "refresh_tokens", "user_profiles", "external_identities"} {
		var count int
		if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s has %d rows after user delete, want 0 (cascade)", table, count)
		}
	}
}

func insertOidcFixtures(t *testing.T, exec func(query string, args ...any) error) {
	t.Helper()
	if err := exec("INSERT INTO user_profiles (user_id, display_name, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"u1", "Dante", testNow, testNow); err != nil {
		t.Fatalf("insert user profile: %v", err)
	}
	if err := exec("INSERT INTO external_identities (id, provider, subject, user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"e1", "GOOGLE", "subject-1", "u1", testNow, testNow); err != nil {
		t.Fatalf("insert external identity: %v", err)
	}
	if err := exec("INSERT INTO external_identities (id, provider, subject, user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"e2", "GOOGLE", "subject-1", "u1", testNow, testNow); err == nil {
		t.Error("duplicate (provider, subject) identity not rejected")
	}
}

func TestAuthMigrationDownRestoresSkeleton(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()
	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	sub, err := fs.Sub(migrations.SQLite, "sqlite")
	if err != nil {
		t.Fatalf("sub migrations: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, database.SQL(), sub, goose.WithLogger(goose.NopLogger()))
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	dropToSkeleton(ctx, t, provider, database)

	if columns := tableColumns(t, database, "users"); !slices.Equal(columns, []string{"id", "email", "created_at", "updated_at"}) {
		t.Errorf("users columns after Down = %v, want the 0001 shape", columns)
	}
	for range 6 {
		if _, err := provider.Up(ctx); err != nil {
			t.Fatalf("Up() after Down error = %v", err)
		}
	}
	if !tableExists(t, database, "password_credentials") || !tableExists(t, database, "user_profiles") {
		t.Error("auth tables missing after re-Up")
	}
}

// dropToSkeleton walks every migration down and checks what survived.
func dropToSkeleton(ctx context.Context, t *testing.T, provider *goose.Provider, database *db.Database) {
	t.Helper()
	steps := []struct {
		version string
		gone    []string
	}{
		{"0007", nil},
		{"0006", []string{"email_verification_tokens", "password_reset_tokens"}},
		{"0005", []string{"user_account_deletion_audits"}},
		{"0004", []string{"stored_files"}},
		{"0003", []string{"user_profiles", "external_identities"}},
		{"0002", []string{"password_credentials"}},
	}
	for _, step := range steps {
		if _, err := provider.Down(ctx); err != nil {
			t.Fatalf("Down() %s error = %v", step.version, err)
		}
		for _, table := range step.gone {
			if tableExists(t, database, table) {
				t.Errorf("%s still present after Down %s", table, step.version)
			}
		}
	}
}

func TestIsUniqueViolation(t *testing.T) {
	database := testDatabase(t)
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	insert := func(query string, args ...any) error {
		if _, err := database.SQL().ExecContext(t.Context(), query, args...); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
		return nil
	}
	if err := insert("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)", "u1", "a@b.co", testNow, testNow); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	duplicate := insert("INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)", "u2", "a@b.co", testNow, testNow)
	if duplicate == nil || !db.IsUniqueViolation(duplicate) {
		t.Fatalf("duplicate insert error = %v, want a unique violation", duplicate)
	}
	foreignKey := insert("INSERT INTO sessions (id, user_id, last_seen_at, expires_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		"s1", "missing-user", testNow, testNow, testNow, testNow)
	if foreignKey == nil || db.IsUniqueViolation(foreignKey) {
		t.Fatalf("fk insert error = %v, want a non-unique failure", foreignKey)
	}
}

func tableExists(t *testing.T, database *db.Database, name string) bool {
	t.Helper()
	var count int
	if err := database.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&count); err != nil {
		t.Fatalf("table %s lookup: %v", name, err)
	}
	return count > 0
}

func indexExists(t *testing.T, database *db.Database, name string) bool {
	t.Helper()
	var count int
	if err := database.SQL().QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?", name).Scan(&count); err != nil {
		t.Fatalf("index %s lookup: %v", name, err)
	}
	return count > 0
}

func tableColumns(t *testing.T, database *db.Database, table string) []string {
	t.Helper()
	rows, err := database.SQL().QueryContext(t.Context(), "PRAGMA table_info("+table+")")
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var (
			cid     int
			name    string
			typ     string
			notNull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info rows: %v", err)
	}
	return columns
}
