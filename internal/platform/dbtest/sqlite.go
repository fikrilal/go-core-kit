//go:build !pg

package dbtest

import (
	"path/filepath"
	"testing"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

// Open returns a migrated database for the test, and closes it on cleanup.
func Open(t *testing.T) *db.Database {
	t.Helper()
	database := db.MustOpen(filepath.Join(t.TempDir(), "test.db"))
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(t.Context(), migrations.SQLite); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

// Rebind is the identity on the SQLite lane: its placeholders are already `?`.
func Rebind(query string) string { return db.Rebind(query) }
