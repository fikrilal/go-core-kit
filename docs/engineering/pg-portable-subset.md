# Postgres lane: portable subset audit

The pg runtime lane (7.1) lands behind the `pg` build tag. This audit sorts every
file that carries `//go:build !pg` today into three verdicts, so the lift is a
decision list rather than a guess. Counts are asserted by
`tools/gokit/portable_test.go`; they may shrink as the lane lands, never grow
silently.

**Totals: 3 lift, 6 stay, 0 switch (9 hand-written tagged paths; the generated half's tags are the generator's business and are not counted).**

## Lifts (3)

Engine-agnostic code still carrying a tag: the lanes' bootstrap files and the integration tests that name an engine to run.

- `cmd/worker/main_test.go`
- `internal/platform/db/auth_schema_test.go`
- `internal/platform/db/gen_integration_test.go`
## Stays (6)

SQLite-specific by construction: the SQLite `Database`, the lane files that name an engine, and the tests that assert engine-owned behaviour. The pg lane has counterparts.

- `internal/platform/config/database_sqlite.go`
- `internal/platform/config/lane_sqlite_test.go`
- `internal/platform/db/sqlite.go`
- `internal/platform/db/sqlite_test.go`
- `internal/platform/dblane/lane_sqlite.go`
- `internal/platform/dbtest/sqlite.go`
## Switches (0)

Tag-free in the end; `internal/platform/dblane` carries the choice, so this bucket is empty.

## Notes

- Contract tests under `cmd/api` are lifts by dependency, but they need a pg test
  database to run; 7.2's dual-lane matrix provisions it, so they stay tagged until
  then and the audit records that as the sequencing, not the verdict.
- Nothing here changes behaviour on the SQLite lane: the tag work is additive, and
  `make verify` keeps running the SQLite profile unchanged.

## Update — after the lane switch (2026-10-15)

`cmd/api/main.go` is no longer a switch: `internal/platform/dblane` exposes `Open`
and `Migrations` per build tag, and both composition roots use it, so the switch
bucket is empty and the roots are tag-free. The remaining lifts are the `cmd/api`
contract tests and the me-feature integration test, which keep their tag until
their hand-written SQL routes through `dbtest.Rebind`.

## Update — the contract suites lifted (2026-10-15)

The `cmd/api` contract tests and the me-feature integration test now run on **both**
lanes: their hand-written SQL goes through `dbtest.Rebind`, and `platform/db`
exports `Rebind` per engine so production code that builds SQL by hand (the me
sessions page query) is portable too. Verified against a live `postgres:17`: the
whole `cmd/api` contract suite passes under `-tags pg` with `DATABASE_URL` set.

What remains tagged (4 lift) is the lane's own tests:
- `cmd/worker/main_test.go`
- `internal/features/me/infra/repository_push_test.go`
- `internal/platform/db/auth_schema_test.go`
- `internal/platform/db/gen_integration_test.go`
