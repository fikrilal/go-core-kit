//go:build !pg

package db_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
)

func testDatabase(t *testing.T) *db.Database {
	t.Helper()
	database := db.MustOpen(filepath.Join(t.TempDir(), "test.db"))
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return database
}

func TestOpenAppliesPragmas(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()

	var journalMode string
	if err := database.SQL().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := database.SQL().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := database.SQL().QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("read busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busyTimeout)
	}

	var synchronous int
	if err := database.SQL().QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("read synchronous: %v", err)
	}
	if synchronous != 1 {
		t.Errorf("synchronous = %d, want 1", synchronous)
	}
}

func TestMigrateCreatesSchema(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()

	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() second run error = %v", err)
	}

	now := utctime.Time{Time: time.Now().UTC()}
	if _, err := database.SQL().ExecContext(ctx,
		"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
		"u1", "a@b.co", now, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	var email string
	var createdAt utctime.Time
	if err := database.SQL().QueryRowContext(ctx,
		"SELECT email, created_at FROM users WHERE id = ?", "u1").Scan(&email, &createdAt); err != nil {
		t.Fatalf("select user: %v", err)
	}
	if email != "a@b.co" {
		t.Errorf("email = %q, want a@b.co", email)
	}
	if !createdAt.Equal(now.Time) {
		t.Errorf("created_at = %v, want %v", createdAt.Time, now.Time)
	}
}

func TestInTxCommitsAndRollsBack(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()
	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	now := utctime.Time{Time: time.Now().UTC()}

	err := database.InTx(ctx, func(tx db.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
			"u1", "a@b.co", now, now); err != nil {
			return fmt.Errorf("insert u1: %w", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("InTx() commit error = %v", err)
	}

	sentinel := errors.New("rollback this")
	err = database.InTx(ctx, func(tx db.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
			"u2", "c@d.co", now, now); err != nil {
			return fmt.Errorf("insert u2: %w", err)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx() error = %v, want sentinel", err)
	}

	var count int
	if err := database.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Errorf("user count = %d, want 1 (rollback failed)", count)
	}
}

func TestPingFailsAfterClose(t *testing.T) {
	database := testDatabase(t)
	if err := database.Ping(t.Context()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := database.Ping(t.Context()); err == nil {
		t.Fatal("Ping() after Close() = nil, want error")
	}
}

func TestInTxRecoversAfterPanic(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the panic to propagate out of InTx")
			}
		}()
		_ = database.InTx(ctx, func(db.Tx) error {
			panic("boom")
		})
	}()

	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var one int
	if err := database.SQL().QueryRowContext(qctx, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("query after recovered panic: %v", err)
	}
}

func TestTimestampsSortChronologically(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()
	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	base := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	rows := []struct {
		id string
		at time.Time
	}{
		{"zero", base},
		{"one-ns", base.Add(time.Nanosecond)},
		{"hundred-ms", base.Add(100 * time.Millisecond)},
		{"hundred-fifty-ms", base.Add(150 * time.Millisecond)},
	}
	for _, row := range rows {
		ts := utctime.Time{Time: row.at}
		if _, err := database.SQL().ExecContext(ctx,
			"INSERT INTO users (id, email, created_at, updated_at) VALUES (?, ?, ?, ?)",
			row.id, row.id+"@example.com", ts, ts); err != nil {
			t.Fatalf("insert %s: %v", row.id, err)
		}
	}

	result, err := database.SQL().QueryContext(ctx, "SELECT id FROM users ORDER BY created_at ASC, id ASC")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = result.Close() }()
	var order []string
	for result.Next() {
		var id string
		if err := result.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		order = append(order, id)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	want := []string{"zero", "one-ns", "hundred-ms", "hundred-fifty-ms"}
	if !slices.Equal(order, want) {
		t.Errorf("ORDER BY created_at ASC -> %v, want %v", order, want)
	}
}
