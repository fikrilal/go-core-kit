//go:build pg

// Package db provides the platform database handle for the compiled engine.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

// serializationFailure is Postgres' SQLSTATE for a serialization conflict, the
// signal a serializable transaction must be retried.
const serializationFailure = "40001"

// transactionAttempts bounds the retry loop: a conflict is expected under
// concurrency, an unbounded loop is not.
const transactionAttempts = 3

// Database is the platform handle to the compiled database engine.
type Database struct {
	sql *sql.DB
}

// Open opens the Postgres database behind url. The pool is deliberately modest:
// one process serves one deployment, and the API and worker each keep their own.
func Open(url string) (*Database, error) {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return &Database{sql: sqlDB}, nil
}

// MustOpen is Open for composition roots; it panics on failure.
func MustOpen(url string) *Database {
	database, err := Open(url)
	if err != nil {
		panic(err)
	}
	return database
}

// SQL exposes the handle for generated queries and transactions.
func (d *Database) SQL() *sql.DB {
	return d.sql
}

// Ping verifies connectivity for readiness checks.
func (d *Database) Ping(ctx context.Context) error {
	if err := d.sql.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Close closes the database handle.
func (d *Database) Close() error {
	if err := d.sql.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// Tx is the transaction surface generated queries accept.
type Tx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrNoRows reports that a query returned no rows.
var ErrNoRows = sql.ErrNoRows

// IsUniqueViolation reports whether err is a uniqueness constraint violation.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505"
}

// InTx runs fn inside a serializable transaction, retrying the whole transaction
// when the server reports a serialization conflict. The closure may therefore run
// more than once: it must not write anything outside the transaction it is given.
func (d *Database) InTx(ctx context.Context, fn func(tx Tx) error) error {
	var lastErr error
	for attempt := 1; attempt <= transactionAttempts; attempt++ {
		err := d.runSerializable(ctx, fn)
		if err == nil {
			return nil
		}
		if !IsSerializationFailure(err) {
			return err
		}
		lastErr = err
		if attempt == transactionAttempts {
			break
		}
		// A short, jittered pause between attempts only: waiting after the last
		// one would delay the failure and could mask it with a context error.
		select {
		case <-ctx.Done():
			return fmt.Errorf("transaction retry: %w", ctx.Err())
		case <-time.After(RetryDelay(attempt)):
		}
	}
	return fmt.Errorf("transaction conflicted %d times: %w", transactionAttempts, lastErr)
}

// RetryDelay grows with the attempt and carries jitter, so transactions that
// conflicted with each other do not retry in lockstep.
func RetryDelay(attempt int) time.Duration {
	base := time.Duration(attempt) * 10 * time.Millisecond
	half := base / 2
	return half + time.Duration(rand.Int64N(int64(half)+1)) //nolint:gosec // backoff jitter, not a secret
}

// runSerializable is one serializable attempt.
func (d *Database) runSerializable(ctx context.Context, fn func(tx Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// IsSerializationFailure reports whether err is a serialization conflict.
func IsSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == serializationFailure
}

// Migrate applies the pg directory in fsys. This build always reads that
// directory. The build tag selects the engine.
func (d *Database) Migrate(ctx context.Context, fsys fs.FS) error {
	sub, err := fs.Sub(fsys, "pg")
	if err != nil {
		return fmt.Errorf("sub migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, d.sql, sub, goose.WithLogger(goose.NopLogger()))
	if err != nil {
		return fmt.Errorf("new goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
