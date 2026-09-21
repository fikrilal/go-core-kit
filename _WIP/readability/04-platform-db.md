# Section note: `internal/platform/db`

- Status: Draft, waiting for acceptance
- Date: 2026-10-11
- Scope: `internal/platform/db` (including `utctime`), hand-written `db/migrations`, hand-written `db/queries`. `sqlc.yaml` only where it decides the Go type of a column.
- Out of scope: line-by-line review of `internal/db/gen`. One generated struct was opened to see how a nullable timestamp is declared.
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: none in this pass
- Earlier notes stay unapplied

## Verdict

This boundary is already the readable one. Features do not import `modernc.org/sqlite` or build SQL strings. They call `InTx`, `IsUniqueViolation`, and generated functions whose text sits in `db/queries/common`. `utctime.Time` is the shared type that makes SQLite `TEXT` sort in time order. The schema tests check the rules the tables actually enforce: checks, unique keys, foreign keys, cascade, and the SQLite down migration.

`InTx` is the function to copy. Begin, run, commit, and the deferred rollback lets a panic release the single connection. The test proves that.

Two things a junior cannot see from the names:

- The SQLite open policy is one DSN string. ADR 0004 is the sentence. The code does not repeat it.
- `utctime.Time` zero means two different database values. `Scan(nil)` is SQL NULL. `Value()` of zero is the text `0001-01-01T00:00:00.000000000Z`. The repository treats `IsZero` as NULL. A `WHERE column IS NULL` predicate does not.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `internal/platform/db/sqlite.go` | 120 | Open, ping, close, tx, unique check, goose |
| `internal/platform/db/sqlite_test.go` | 223 | Pragmas, migrate, tx, panic, timestamp order |
| `internal/platform/db/auth_schema_test.go` | 242 | Auth tables, checks, cascade, down migration |
| `internal/platform/db/gen_integration_test.go` | 49 | `CreateUser` / `GetUserByID` round trip |
| `internal/platform/db/utctime/utctime.go` | 53 | Scan and Value |
| `internal/platform/db/utctime/utctime_test.go` | 98 | UTC, nil, garbage, SQLite formats, sort width |
| `db/migrations/migrations.go` | 14 | Embedded sqlite and pg sets |
| `db/migrations/sqlite/*.sql` | 3 files | Users, auth, OIDC |
| `db/migrations/pg/*.sql` | 3 files | Same shape, `TIMESTAMPTZ` |
| `db/queries/common/users.sql` | 9 | Create and get the early user row |
| `db/queries/common/auth.sql` | 211 | Credentials, sessions, refresh, OIDC, me |

There is no `//go:build pg` file under `internal/platform/db`. `sqlite.go` is `//go:build !pg`. Postgres migrations are embedded and unused by this process. Roadmap task 7 is still open. Not a defect in the SQLite code.

The sqlite and pg `Up` sections were compared column by column. Names, nullability, checks, uniques, and indexes match. The only type difference is `TEXT` versus `TIMESTAMPTZ`. The down sections differ because SQLite cannot `DROP COLUMN` on the checked user columns, so `0002` rebuilds `users`. That comment is the right kind. `TestAuthMigrationDownRestoresSkeleton` walks down and back up.

## Hard to read

### 1. The open policy is a query string

`sqlite.go` `Open` builds one DSN:

```text
file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate
```

Then `SetMaxOpenConns(1)`. ADR 0004 names this: WAL, `busy_timeout`, single-writer `BEGIN IMMEDIATE`. Plan 1.9a says the driver timeout is the busy retry, so there is no second retry loop. The tests read `journal_mode`, `foreign_keys`, `busy_timeout`, and `synchronous` back. Nothing reads `_txlock`, so deleting `immediate` stays green.

`synchronous=1` in the test is SQLite's number for `NORMAL`. The DSN says `NORMAL`. The assertion says `1`. A reader has to know the encoding.

Name the pieces. Suggested shape, same DSN:

```go
const (
    busyTimeout = "5000"
    // synchronousNORMAL is what PRAGMA synchronous returns for NORMAL.
    synchronousNORMAL = 1
)
```

And one comment on `SetMaxOpenConns(1)`: one connection is the write lock. `busy_timeout` only helps a second process, not a second connection in this process. Do not raise the pool on SQLite without replacing this comment.

### 2. `Migrate` always opens the `sqlite` directory

```go
sub, err := fs.Sub(fsys, "sqlite")
```

The argument looks like any migration set. `migrations.Postgres` has no `sqlite` folder, so this call cannot apply it. The build tag is what selects the engine, not the argument. Say that on the function. The pg build, when it exists, gets its own file. Do not add a dialect switch here.

### 3. Zero time and SQL NULL are not the same write

`utctime.Time.Scan(nil)` stores the zero time. `IsZero()` is true. `Value()` does not return SQL NULL. Probed 2026-10-11:

```text
zero Value="0001-01-01T00:00:00.000000000Z"
roundTrip IsZero=true
null Scan IsZero=true
```

`internal/features/auth/infra/repository.go` uses `!timestamp.IsZero()` for email verification and for revoked session and refresh rows. That matches a NULL read. It does not match a row whose column holds year 1.

`auth.sql` treats "not revoked" as `revoked_at IS NULL`. An update that binds a zero `utctime.Time` stores year 1. Go still says `IsZero`. The `IS NULL` predicate skips the row, so the revoke statements do not touch it. No current query does that write. Inserts omit `revoked_at`. Updates pass `now` from the injected clock.

sqlc is why this is easy to miss. Nullable strings become `sql.NullString` (`GetMeView` profile names). Nullable timestamps are overridden in `sqlc.yaml` to `utctime.Time`, not a pointer. Every `TEXT` time column in the sqlite migrations is listed there. A new `TEXT` timestamp that is missing from that list becomes a plain string, and the repository grows a second time type.

`users.email_verified_at`, `users.suspended_at`, `sessions.revoked_at`, `refresh_tokens.revoked_at`, and the push-token times are nullable. `created_at` and `expires_at` are not. `Value()` returning nil for `IsZero` would make an accidental zero fail a `NOT NULL` insert, and would make a nullable column actually NULL. That is the fix. It changes what a zero value stores. It does not change a normal `now` write.

### 4. `GetUserByID` is not the user

`db/queries/common/users.sql` selects `id, email, created_at, updated_at`. Role, status, and `email_verified_at` are absent. Defaults for role and status live only in `0002_auth.sql`.

The only production caller is `ChangePasswordAndRevokeOtherSessions`, and it only checks `ErrNoRows`. The name still reads as "load the user". `GetUserForLogin` and `GetMeView` are the reads that carry the account. When the auth feature is reviewed, do not extend `GetUserByID` into a partial user. Existence can stay a tiny query. The shape should not grow a second user record.

### 5. Empty string is NULL, in several statements

`CreateSession`, `TouchSession`, `MarkRefreshTokenRotated`, `CreateExternalIdentity`, and `CreateUserProfile` use `NULLIF(sqlc.arg(...), '')`. A blank device name, IP, active key, or profile name is stored as NULL. A caller cannot persist an empty string. That is a real policy and it is repeated instead of explained. One comment at the top of `auth.sql` is enough. Do not invent a Go helper that hides the SQL.

The revoke statements repeat the same column list: clear `active_key`, `push_platform`, `push_token`, and stamp the push revoked time. `RevokeSessionByActiveKey` and `RevokeOtherSessions` are copies. Leave the copies. A shared SQL fragment would be the clever version. The list is the behavior.

`RevokeOtherSessions` and `RevokeOtherSessionRefreshTokens` are one policy split across two statements. The repository transaction is what keeps them together. That pairing belongs in the auth-feature note, not in a database trigger.

## Correct Go that should stay

- `Database` hides `*sql.DB`. `SQL()` is the seam `gen.New` needs. Features that have a `*Database` can still run a raw statement. `cmd/api` contract tests do. Production feature code goes through `InTx` and query functions. Do not remove `SQL()`. Do not add a query builder.
- `ErrNoRows` is `sql.ErrNoRows` under a name the feature can use without importing `database/sql` for the sentinel. `errors.Is` works through wraps. `IsUniqueViolation` uses `errors.As` on `*sqlite.Error` and accepts both unique and primary-key codes. The test wraps the insert error and also shows a foreign-key failure is not unique. Keep that test. The sqlite driver import stays in this package.
- `InTx` returns the function error unwrapped, and wraps begin and commit. The sentinel rollback test depends on the unwrap. The deferred `Rollback` ignores its error, including `sql.ErrTxDone` after a successful commit. A failed rollback is discarded. Acceptable. The panic test is the one that matters under `MaxOpenConns(1)`: a stuck transaction is an outage.
- `MustOpen` panics. Tests use it. `cmd/api` uses `Open` and returns the error. Same split as `config.MustLoad`. Leave both.
- `utctime` normalizes every successful scan to UTC. `Value` formats UTC with a fixed fractional width so `ORDER BY created_at` works on `TEXT`. `TestTimestampsSortChronologically` inserts a 1 nanosecond gap and a 100 vs 150 millisecond gap. That test is the reason the layout exists. `Scan` also accepts the looser SQLite datetime strings. Plan 1.9a calls that defensive. Our own writes never need it. Leave the parser.
- Migrations are goose `Up` / `Down`, one concern per file: skeleton user, auth, OIDC. Checks for role, status, push platform, and OIDC provider are in the table, not only in Go. Cascade is tested by deleting the user.
- Shared queries use `sqlc.arg`, `RETURNING`, and no `ILIKE`, arrays, or `FOR UPDATE`. That is the ADR 0005 subset. Both engines compile the same files. There is no `db/queries/sqlite` or `db/queries/pg` yet, because nothing needs a dialect query.
- `GetAuthMethods` is a `UNION` of the literal `PASSWORD` and the provider column. The provider check allows only `GOOGLE`. The query is the whole rule. Leave it.
- Schema tests use string concatenation for table names that are literals in the test. They are not a product path.

## Real bugs

### B1. `Value` of the zero time is not NULL

Quoted above. Live rows are not wrong today. The next statement that binds a zero `utctime.Time` into `revoked_at`, `email_verified_at`, or `suspended_at` stores year 1, and the `IS NULL` queries disagree with `IsZero`.

When accepted: `Value` returns `nil` for `IsZero`, and a test locks both directions. Scan of NULL stays zero. Scan of the year-1 string can keep meaning zero so old rows, if any were written, still read as empty. A `NOT NULL` column given a zero time should fail the insert.

### B2. There is no schema-parity check yet

ADR 0005 says CI compares the table and column inventory of the two migration trees. Roadmap 7.2 is unchecked. I compared the current three files by hand and they match. Nothing fails the build if the next migration edits sqlite and forgets pg. That is a process bug, not a bad row. Do not hand-write a checker inside a readability pass. Record it so the pg engine work does not start by assuming the guard exists.

## Design question

What is still in the feature, and what must stay here?

The engine stays here: pragmas, the single connection, goose, the sqlite error code, and the time type. The feature repository stays the orchestrator. `RotateRefreshToken` and `ChangePasswordAndRevokeOtherSessions` already open `InTx` and call several query functions. That is the mobile repository shape: the SQL files are the statements, the Go function is the sequence. Do not move those sequences into `platform/db`.

Do not add an ORM, a query helper, or a `NullTime` wrapper beside `utctime.Time` unless B1 cannot be fixed on `Value`. One type is the point.

`GetUserByID` staying narrow is a query decision. Changing the call site is the auth-feature note.

## Function and query notes

### `sqlite.go`

- `Open`, `MustOpen`. Finding 1. `sql.Open` does not connect. A missing parent directory fails on migrate, which `cmd/api` runs before listen. Early enough.
- `SQL`, `Ping`, `Close`. Clean.
- `Tx`, `ErrNoRows`, `IsUniqueViolation`. Clean.
- `InTx`. Clean. `BeginTx` with nil options plus `_txlock=immediate` is `BEGIN IMMEDIATE`.
- `Migrate`. Finding 2. `goose.NopLogger` hides the progress line. The returned error still names the failure. Leave the logger quiet.

### `utctime.go`

Finding 3 and bug B1. The scan switch is the readable part: nil, `time.Time`, string, bytes, otherwise an error that includes the Go type. `parse` tries three layouts and UTC-normalizes the one that works.

### Tests

`sqlite_test.go` covers pragmas except the immediate lock, migrate idempotence, commit and rollback, ping after close, panic recovery, and chronological order.

`auth_schema_test.go` covers table presence, the auth columns, the indexes, role and status and push checks, `active_key` uniqueness, the refresh-token foreign key, cascade, OIDC uniqueness, and down/up. It does not insert `provider = 'NOPE'`. The check is in the migration. Low.

`gen_integration_test.go` covers the early user query only. Auth queries are exercised through feature and contract tests, not here.

`utctime_test.go` covers offset to UTC, nil, garbage, two SQLite datetime strings, and fixed width. It does not cover bug B1.

### `migrations.go`

Clean. The embed paths match the goose files. A one-line package comment that the two trees differ only by timestamp type, and that pg is not opened yet, would stop a reader from hunting a pg `Open`.

### `users.sql`

Finding 4. `CreateUser` relies on column defaults for `role` and `status`.

### `auth.sql`

Finding 5. Each statement is one named query. The login read is a left join so a user without a password still returns. Session create uses `NULLIF` on optional device fields. Refresh rotation's compare-and-set is `revoked_at IS NULL AND replaced_by_id IS NULL` in `MarkRefreshTokenRotated`. That predicate is the reuse rule. Do not simplify it.

`RevokeSession` does not clear device columns. `ClearSessionDeviceState` does. Logout's repository is responsible for calling both. Same note as the other pair: feature section, not a trigger.

## What acceptance authorizes

Taste, no schema change:

1. Name the DSN pieces and comment `MaxOpenConns(1)` and `_txlock=immediate`.
2. Comment that `Migrate` on this build reads the `sqlite` directory only.
3. Comment on `utctime.Time` that `IsZero` means NULL on read, and point at bug B1 for the write.
4. One comment at the top of `auth.sql` for `NULLIF(..., '')`.
5. Comment on `migrations.go` that pg is embedded and not opened.

Bugs, separate yes:

- B1. `Value` returns nil when `IsZero`. Test the NULL write and the NOT NULL failure.
- B2. Leave the schema-inventory checker to the pg engine task. Do not fake it with a comment.

Not in this acceptance:

- A pg `Open`, pool, or `40001` retry.
- Folding revoke statements into one query or a trigger.
- Widening `GetUserByID`.
- Editing generated sqlc output by hand.
- Applying the earlier notes.

## Verification

No production edit. `make verify` was not run.

Probe, deleted afterward (`internal/platform/db/utctime/probe_audit_test.go` is not in the tree):

```bash
go test ./internal/platform/db/utctime -run TestAuditZeroValueProbe -count=1
```

Result: pass. The zero value and the `IsZero` flags are quoted in finding 3. The probe is not coverage.
