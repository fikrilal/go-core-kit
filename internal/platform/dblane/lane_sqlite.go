//go:build !pg

// Package dblane selects the compiled engine's database handle and migration tree
// for the composition roots, so they never name one engine's key.
package dblane

import (
	"fmt"
	"io/fs"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

// Open opens the database the compiled engine uses.
func Open(cfg config.Config) (*db.Database, error) {
	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open the sqlite lane's database: %w", err)
	}
	return database, nil
}

// Migrations returns the embedded migration tree for the compiled engine.
func Migrations() fs.FS { return migrations.SQLite }
