// Package dbtest opens a database for tests on whichever engine the lane
// compiled: a temporary SQLite file, or a schema of its own inside the Postgres
// server DATABASE_URL points at.
package dbtest
