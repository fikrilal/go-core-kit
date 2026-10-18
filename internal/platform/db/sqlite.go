//go:build !pg

// Package db provides the platform database handle for the compiled engine.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Database is the platform handle to the compiled database engine.
type Database struct {
	sql *sql.DB
}

// Open opens the SQLite database at path with the platform's pragmas and write policy.
// One connection is the write lock. busy_timeout helps another process, not a
// second connection here. _txlock=immediate makes BeginTx with nil options BEGIN IMMEDIATE.
func Open(path string) (*Database, error) {
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_txlock=immediate"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	return &Database{sql: sqlDB}, nil
}

// MustOpen is Open for composition roots; it panics on failure.
func MustOpen(path string) *Database {
	database, err := Open(path)
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

// Rebind returns query unchanged: SQLite's placeholders are already `?`. It
// exists so hand-built SQL can be written once for both engines.
func Rebind(query string) string { return query }

// ErrNoRows reports that a query returned no rows.
var ErrNoRows = sql.ErrNoRows

// IsUniqueViolation reports whether err is a uniqueness constraint violation.
func IsUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	code := sqliteErr.Code()
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

// InTx runs fn inside a write transaction, rolling back when fn fails.
func (d *Database) InTx(ctx context.Context, fn func(tx Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
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

// Migrate applies the sqlite directory in fsys. This build always reads that
// directory. The build tag selects the engine.
func (d *Database) Migrate(ctx context.Context, fsys fs.FS) error {
	sub, err := fs.Sub(fsys, "sqlite")
	if err != nil {
		return fmt.Errorf("sub migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, d.sql, sub, goose.WithLogger(goose.NopLogger()))
	if err != nil {
		return fmt.Errorf("new goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
