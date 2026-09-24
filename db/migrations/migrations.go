// Package migrations embeds the per-engine goose migration sets.
// The trees differ by timestamp type, TEXT versus TIMESTAMPTZ.
// This process embeds Postgres and does not open it.
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
