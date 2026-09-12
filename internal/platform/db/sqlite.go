//go:build !pg

// Package db provides the platform database handle for the compiled engine.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Database is the platform handle to the compiled database engine.
type Database struct {
	sql *sql.DB
}

// Open opens the SQLite database at path with the platform's pragmas and write policy.
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

// InTx runs fn inside a write transaction, rolling back when fn fails.
func (d *Database) InTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
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

// Migrate applies the embedded goose migration set.
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
