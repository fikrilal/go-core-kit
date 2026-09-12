// Package migrations embeds the per-engine goose migration sets.
package migrations

import "embed"

// SQLite holds the SQLite migration set.
//
//go:embed sqlite/*.sql
var SQLite embed.FS

// Postgres holds the Postgres migration set.
//
//go:embed pg/*.sql
var Postgres embed.FS
