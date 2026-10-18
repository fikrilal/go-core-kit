# Postgres lane: portable subset audit

The pg runtime lane (7.1) lands behind the `pg` build tag. This audit sorts every
file that carries `//go:build !pg` today into three verdicts, so the lift is a
decision list rather than a guess. Counts are asserted by
`tools/gokit/portable_test.go`; they may shrink as the lane lands, never grow
silently.

**Totals: 29 lift, 10 stay, 1 switch (40 tagged paths).

## Lifts (29)

Engine-agnostic code: it talks to `ports`, `gen`, or the platform only, so the pg
lane compiles it unchanged.

- `cmd/api/contract_auth_test.go`
- `cmd/api/contract_email_resend_test.go`
- `cmd/api/contract_email_verify_test.go`
- `cmd/api/contract_jwks_test.go`
- `cmd/api/contract_me_deletion_test.go`
- `cmd/api/contract_me_profile_image_test.go`
- `cmd/api/contract_me_push_test.go`
- `cmd/api/contract_me_sessions_test.go`
- `cmd/api/contract_me_test.go`
- `cmd/api/contract_oidc_connect_test.go`
- `cmd/api/contract_oidc_test.go`
- `cmd/api/contract_password_change_test.go`
- `cmd/api/contract_password_reset_request_test.go`
- `cmd/api/contract_password_reset_test.go`
- `cmd/api/contract_refresh_test.go`
- `cmd/api/contract_test.go`
- `cmd/api/coverage_test.go`
- `cmd/api/helpers_test.go`
- `cmd/api/server.go`
- `cmd/api/server_test.go`
- `cmd/worker/main.go`
- `cmd/worker/main_test.go`
- `cmd/worker/server.go`
- `internal/features/auth/auth.go`
- `internal/features/auth/infra/repository.go`
- `internal/features/auth/jobs.go`
- `internal/features/me/infra/repository.go`
- `internal/features/me/infra/repository_push_test.go`
- `internal/features/me/me.go`

## Stays (10)

SQLite-specific by construction: the generated SQLite query half and the SQLite
`Database` with its integration tests. The pg lane gets counterparts
(`pg_*.go`, `pg.go`, and its own guarded tests) rather than these files losing
their tag.

- `internal/db/gen/sqlite_auth.sql.go`
- `internal/db/gen/sqlite_db.go`
- `internal/db/gen/sqlite_me.sql.go`
- `internal/db/gen/sqlite_models.go`
- `internal/db/gen/sqlite_users.sql.go`
- `internal/platform/db/auth_schema_test.go`
- `internal/platform/db/gen_integration_test.go`
- `internal/platform/db/sqlite.go`
- `internal/platform/config/database_sqlite.go` — the SQLite lane's required-key check
- `internal/platform/db/sqlite_test.go`

## Switches (1)

Tag-free in the end, selecting its engine by build tag instead:

- `cmd/api/main.go` — applies the embedded migrations; it must pick `migrations.SQLite` or `migrations.Postgres` per lane instead of naming one.

## Notes

- Contract tests under `cmd/api` are lifts by dependency, but they need a pg test
  database to run; 7.2's dual-lane matrix provisions it, so they stay tagged until
  then and the audit records that as the sequencing, not the verdict.
- Nothing here changes behaviour on the SQLite lane: the tag work is additive, and
  `make verify` keeps running the SQLite profile unchanged.
