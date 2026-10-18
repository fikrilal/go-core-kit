//go:build pg

package dbtest

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

// Open returns a migrated database for the test: a schema of its own inside the
// server DATABASE_URL names, dropped on cleanup, so tests do not share state. It
// skips loudly without a URL rather than passing silently.
func Open(t *testing.T) *db.Database {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is unset: the pg lane's tests run where a server is available")
	}
	ctx := t.Context()
	schemaID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("schema id: %v", err)
	}
	schema := "test_" + strings.ReplaceAll(schemaID.String(), "-", "")
	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open admin handle: %v", err)
	}
	// #nosec G201 -- the identifier is a generated UUID, never caller input.
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		defer func() { _ = admin.Close() }()
		cleanupCtx := context.WithoutCancel(t.Context())
		// #nosec G201 -- the identifier is a generated UUID, never caller input.
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Logf("drop schema %s: %v", schema, err)
		}
	})

	database, err := db.Open(scopedURL(url, schema))
	if err != nil {
		t.Fatalf("open scoped database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx, migrations.Postgres); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

// Rebind converts the portable `?` placeholders to the lane's style. Tests write
// one query text for both engines; only the pg lane rewrites it.
func Rebind(query string) string { return db.Rebind(query) }

// scopedURL pins the connection's search_path to the test's schema, so every
// migration and query lands there without touching the others.
func scopedURL(url, schema string) string {
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	return url + separator + "search_path=" + schema
}
