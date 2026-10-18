//go:build pg

package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

// TestPGRoundTrip exercises the pg handle against a live server: migrations from
// the pg tree, one committed transaction, and one that rolls back. Without a
// database URL it skips loudly — the dual-lane matrix is 7.2's job, and a test
// that silently passes on an unconfigured box is worse than one that is absent.
func TestPGRoundTrip(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is unset: run the pg lane where a server is available")
	}
	ctx := t.Context()
	database, err := db.Open(url)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if err := database.Migrate(ctx, migrations.Postgres); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	if err := database.InTx(ctx, func(tx db.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
			return fmt.Errorf("exec select 1: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("InTx() error = %v", err)
	}

	wantErr := errors.New("rollback")
	if err := database.InTx(ctx, func(tx db.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT 1"); err != nil {
			return fmt.Errorf("exec select 1: %w", err)
		}
		return wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("InTx(rollback) error = %v, want the caller's error", err)
	}
}

// TestIsUniqueViolation classifies the pg error the repositories switch on.
func TestIsUniqueViolation(t *testing.T) {
	if !db.IsUniqueViolation(&pgconn.PgError{Code: "23505"}) {
		t.Error("IsUniqueViolation(23505) = false, want true")
	}
	if db.IsUniqueViolation(&pgconn.PgError{Code: "23503"}) {
		t.Error("IsUniqueViolation(23503) = true, want false")
	}
	if db.IsUniqueViolation(errors.New("boom")) {
		t.Error("IsUniqueViolation(non-pg error) = true, want false")
	}
}

// TestIsSerializationFailure classifies what InTx retries.
func TestIsSerializationFailure(t *testing.T) {
	if !db.IsSerializationFailure(&pgconn.PgError{Code: "40001"}) {
		t.Error("IsSerializationFailure(40001) = false, want true")
	}
	if db.IsSerializationFailure(&pgconn.PgError{Code: "23505"}) {
		t.Error("IsSerializationFailure(23505) = true, want false")
	}
	if db.IsSerializationFailure(context.DeadlineExceeded) {
		t.Error("IsSerializationFailure(non-pg error) = true, want false")
	}
}

// TestRetryDelayIsJittered checks the bounded, jittered backoff shape.
func TestRetryDelayIsJittered(t *testing.T) {
	for attempt := 1; attempt <= 3; attempt++ {
		base := time.Duration(attempt) * 10 * time.Millisecond
		seen := map[time.Duration]bool{}
		for range 20 {
			delay := db.RetryDelay(attempt)
			if delay < base/2 || delay > base {
				t.Fatalf("RetryDelay(%d) = %v, want within [%v, %v]", attempt, delay, base/2, base)
			}
			seen[delay] = true
		}
		if len(seen) == 1 {
			t.Errorf("RetryDelay(%d) returned one value 20 times: no jitter", attempt)
		}
	}
}

// TestRebindNumbersPlaceholders checks the translation callers rely on.
func TestRebind(t *testing.T) {
	tests := []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"SELECT * FROM users WHERE id = ?", "SELECT * FROM users WHERE id = $1"},
		{"UPDATE t SET a = ?, b = ? WHERE id = ?", "UPDATE t SET a = $1, b = $2 WHERE id = $3"},
	}
	for _, tt := range tests {
		if got := db.Rebind(tt.in); got != tt.want {
			t.Errorf("Rebind(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
