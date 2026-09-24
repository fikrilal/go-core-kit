# N4 — db comments

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 04
- Related: `_WIP/readability/04-platform-db.md`

## Objective

Make the SQLite open policy, the migration directory, the zero-time write, and the blank-string SQL visible. No DSN, schema, or stored-value change.

## Constraints

- The DSN string stays `busy_timeout(5000)`, `journal_mode(WAL)`, `foreign_keys(1)`, `synchronous(NORMAL)`, `_txlock=immediate`, and `SetMaxOpenConns(1)`.
- Do not export a constant for the pragma test. The test package cannot see an unexported name.
- `utctime.Time.Value` of zero still returns the year-1 string. That is bug B1.
- Do not add a schema-inventory checker. That is bug B2 and belongs to the Postgres engine task.
- Do not add a pg `Open`, fold revoke SQL, widen `GetUserByID`, or edit `internal/db/gen`.
- Do not add a Go helper that hides `NULLIF`.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `Open` says one connection is the write lock, `busy_timeout` helps another process, and `_txlock=immediate` makes `BeginTx` with nil options `BEGIN IMMEDIATE`.
2. The pragma test says `1` is what `PRAGMA synchronous` returns for `NORMAL`.
3. `Migrate` says this build reads the `sqlite` directory. The build tag selects the engine.
4. `utctime.Time` says `Scan` of NULL is the zero time, and `Value` of zero still writes year 1.
5. `auth.sql` says `NULLIF` stores a blank optional string as NULL. The comment sits on `CreateSession`, the first statement that uses it.
6. `migrations.go` says the trees differ by timestamp type and this process does not open Postgres.

## Steps

- [x] Comment `Open`, the synchronous assertion, and `Migrate`.
- [x] Comment the zero-time write on `utctime.Time`.
- [x] Comment `NULLIF` and the unused Postgres embed.
- [x] `go test ./internal/platform/db/...` and `make verify`.

## Decision Log

- 2026-10-11: The DSN lines already name each pragma. The missing facts are what `MaxOpenConns(1)`, `_txlock=immediate`, and `synchronous = 1` mean. A new constant would not be visible to `package db_test`.
- 2026-10-11: B1 stays a behavior change. The comment records that `IsZero` and `Value` disagree. B2 is not a comment and not a checker in this plan.
- 2026-10-11: sqlc copies the comment above a query into the generated Go doc and turns two quotes into one curly quote. The sentence therefore says `NULLIF` and sits on `CreateSession`, not above `CreatePasswordCredential`.

## Verification

```bash
go test ./internal/platform/db/... -count=1
make verify
```

Outcome (2026-10-11): `go test ./internal/platform/db/... -count=1` passed. `make verify` passed knowledge, contract, fmt, vet, lint, test, build, and build-pg, then failed at drift. sqlc copied the `CreateSession` comment into `internal/db/gen/sqlite_auth.sql.go` and `internal/db/gen/pg_auth.sql.go`. A second `make generate` does not change those files. Drift stays red until that generated doc is committed. No DSN or stored-value change.

## Completion Notes

`Open` states the single-connection write lock, that `busy_timeout` helps another process, and that `_txlock=immediate` makes `BeginTx` with nil options `BEGIN IMMEDIATE`. The pragma test states that `1` is `PRAGMA synchronous` for `NORMAL`. `Migrate` states that this build reads the `sqlite` directory. `utctime.Time` states that a NULL read is zero and that `Value` of zero still writes year 1. `migrations.go` states that the trees differ by timestamp type and that Postgres is embedded and not opened. The `NULLIF` sentence sits on `CreateSession` because sqlc turns a leading comment into the next query's Go doc and mangles a pair of quotes.

## Follow-Ups

- [ ] B1 `Value` returns nil when `IsZero`, after an explicit yes
- [ ] B2 schema-inventory checker, with the Postgres engine task
