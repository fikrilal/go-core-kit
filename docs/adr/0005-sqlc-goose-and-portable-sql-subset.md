# ADR: sqlc, goose, and the Portable SQL Subset

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

The dual-engine decision (ADR-0004) needs one data-access approach that stays readable, keeps SQL visible, and survives two dialects without feature-level duplication.

## Decision

- **Queries**: `sqlc` with two sql blocks (sqlite, postgresql). Shared queries live in `db/queries/common` and must parse under both engines; dialect-only queries go to `db/queries/pg` or `db/queries/sqlite`.
- **Portable subset**: named arguments (`sqlc.arg` / `sqlc.narg`); IDs generated in Go (UUIDv7); UTC timestamps from the injected clock; `TEXT` + `CHECK` enums; booleans normalized via sqlc type overrides; `RETURNING` and `ON CONFLICT` allowed; `ILIKE`, arrays, `jsonb` operators, `INTERVAL`, `DISTINCT ON`, and `FOR UPDATE` banned in shared queries.
- **Migrations**: `goose`, one set per engine (`db/migrations/{sqlite,pg}`), embedded and selected by the compiled engine.
- **Generation**: `gokit db generate` runs sqlc twice, injects build tags (`!pg` / `pg`), and packages output into `internal/db/gen`; output is committed and drift-gated (`make generate` + `git diff --exit-code`).
- **Parity guard**: a schema-inventory check compares table/column inventories across engines in CI.

## Rationale

SQL is the most readable query language, and sqlc turns it into plain, compile-time-checked Go. Generation fails when a shared query is not portable — the discipline enforces itself. Committed generated artifacts follow the family's drift-gate pattern.

## Consequences

Positive: SQL visible in `.sql` files; type-safe queries; no ORM or reflection; one repository implementation per feature.

Negative: `make generate` is required after query/schema changes; contributors learn the subset rules; two migration files per schema change.

## Alternatives Considered

- `sqlx` — less compile-time safety, repetitive scanning.
- ORM (GORM / ent) — rejected: reflection and hidden SQL.
- Hand-written SQL with manual mapping — verbose and error-prone.

## Links / References

- Related ADRs: ADR-0004
- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-dual-engine-data-layer-proposal.md`
