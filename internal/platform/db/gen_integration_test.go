//go:build !pg

package db_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/db/migrations"
	"github.com/fikrilal/go-core-kit/internal/db/gen"
	"github.com/fikrilal/go-core-kit/internal/platform/db/utctime"
)

func TestGeneratedQueriesRoundtrip(t *testing.T) {
	database := testDatabase(t)
	ctx := t.Context()
	if err := database.Migrate(ctx, migrations.SQLite); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	queries := gen.New(database.SQL())
	now := utctime.Time{Time: time.Now().UTC()}

	created, err := queries.CreateUser(ctx, gen.CreateUserParams{
		ID:        "u1",
		Email:     "a@b.co",
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if created.Email != "a@b.co" || !created.CreatedAt.Equal(now.Time) {
		t.Errorf("created = %+v", created)
	}

	fetched, err := queries.GetUserByID(ctx, "u1")
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}
	if fetched.ID != "u1" || !fetched.UpdatedAt.Equal(now.Time) {
		t.Errorf("fetched = %+v", fetched)
	}

	if _, err := queries.GetUserByID(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetUserByID(missing) error = %v, want sql.ErrNoRows", err)
	}
}
