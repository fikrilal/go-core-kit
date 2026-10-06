package migrations

import (
	"slices"
	"strings"
	"testing"
)

func TestSchemaInventoryMatchesEmbeddedTrees(t *testing.T) {
	sqliteSQL, err := readUpSQL(SQLite)
	if err != nil {
		t.Fatalf("readUpSQL(sqlite) error = %v", err)
	}
	pgSQL, err := readUpSQL(Postgres)
	if err != nil {
		t.Fatalf("readUpSQL(pg) error = %v", err)
	}
	issues := compareInventories(parseUpSQL(sqliteSQL), parseUpSQL(pgSQL))
	t.Logf("embedded tree issues: %d", len(issues))
	if len(issues) != 0 {
		t.Fatalf("sqlite and pg schema names differ:\n%s", strings.Join(issues, "\n"))
	}
	if len(parseUpSQL(sqliteSQL)) == 0 {
		t.Fatal("sqlite inventory is empty")
	}
}

func TestSchemaInventoryRejectsOneSidedNames(t *testing.T) {
	left := parseUpSQL(`
		CREATE TABLE users (id TEXT, email TEXT);
		CREATE TABLE sessions (id TEXT);
	`)
	right := parseUpSQL(`
		CREATE TABLE users (id TEXT);
		CREATE TABLE tokens (id TEXT);
		ALTER TABLE users ADD COLUMN role TEXT;
	`)
	issues := compareInventories(left, right)
	t.Logf("one-sided issues: %s", strings.Join(issues, "; "))
	want := []string{
		"column users.email only on the left",
		"column users.role only on the right",
		"table sessions only on the left",
		"table tokens only on the right",
	}
	if !slices.Equal(issues, want) {
		t.Fatalf("issues = %v, want %v", issues, want)
	}
}

func TestSchemaInventoryIgnoresCommentsAndOptionalAlterWords(t *testing.T) {
	parsed := parseUpSQL(`
		CREATE TABLE users (
			id TEXT, -- unique, do not change
			email TEXT -- don't drop
		);
		CREATE TABLE sessions (id TEXT /* keep, don't */);
		CREATE TABLE labels (
			name TEXT DEFAULT '--not-a-comment',
			note TEXT DEFAULT '/* still a value */'
		);
		-- ALTER TABLE users ADD COLUMN ghost TEXT
		ALTER TABLE users ADD email_status TEXT;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT;
		ALTER TABLE users ADD CONSTRAINT users_email UNIQUE (email);
	`)
	got := map[string][]string{}
	for table, cols := range parsed {
		names := make([]string, 0, len(cols))
		for name := range cols {
			names = append(names, name)
		}
		slices.Sort(names)
		got[table] = names
	}
	want := map[string][]string{
		"users":    {"email", "email_status", "id", "role"},
		"sessions": {"id"},
		"labels":   {"name", "note"},
	}
	for table, names := range want {
		if !slices.Equal(got[table], names) {
			t.Fatalf("%s columns = %v, want %v", table, got[table], names)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("tables = %#v", got)
	}
}
