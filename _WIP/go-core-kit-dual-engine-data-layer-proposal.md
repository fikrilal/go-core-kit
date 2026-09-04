# Go Core Kit Dual-Engine Data Layer Proposal

- Status: Accepted for planning (2026-10-10)
- Date: 2026-10-10
- Scope: data-layer engine strategy — SQLite as the first-choice default with an opt-in Postgres engine selected at build time; portable SQL subset; generation pipeline; migrations; runtime differences; CI and deployment modes
- Non-scope: per-table schema design, implementation, ADRs, execution plans, automatic cross-engine data migration

## Summary

SQLite is go-core-kit's first-choice database: it deletes the database server entirely and keeps the whole backend on one small machine. Some projects will outgrow a single box, so the kit also carries an opt-in Postgres engine.

The engine is chosen at **build time** (`go build` → SQLite, `go build -tags pg` → Postgres), not at runtime. That single decision is what keeps feature code single-sourced: runtime switching would force two repository implementations per feature (see "Decision: build-time engine selection").

One shared query set in a portable SQL subset compiles for both engines. Only migrations and the rare dialect-specific query double. Feature code, handlers, services, tests, and the contract-parity machinery are identical in both builds.

Related: the accepted architecture proposal at `./go-core-kit-architecture-proposal.md`. This proposal supersedes its single-engine data-layer decision.

## Context

- The architecture proposal originally accepted Postgres + `pgx/v5` + `sqlc`. During review, the resource cost of Postgres (40–80 MB baseline, an extra service to run, tune, and back up) was weighed against SQLite (in-process, ~5–20 MB, no service) and the data layer was moved.
- Decision from the discussion: **SQLite first choice**, with a **per-project swap** to Postgres for projects that need horizontal scale or managed infrastructure. This document specifies that swap.
- Family context: backend-core-kit is Postgres-based and remains the contract origin. The Go kit is the lean sibling; the wire contract is engine-independent, so clients never observe the difference.

## Goals

- Default build is SQLite: zero external database, no Docker for dev/tests, smallest possible deployment.
- Opt-in Postgres build for projects that need multi-instance or managed infrastructure.
- One feature code path: repositories, services, and handlers are identical in both builds — no per-engine feature code, ever.
- Machine-enforced discipline: shared queries must parse for both engines, generated code is drift-gated, schema inventories are compared in CI.
- Wire contract unaffected: the parity machinery runs against both engines in CI.

## Non-goals

- Runtime engine switching inside one binary (rejected — see "Alternatives considered").
- MySQL or any other engine.
- Automatic data migration between engines (manual dump/load in v1; a helper is an open question).
- Engine-specific schema features (native enums, arrays, `jsonb` operators) in shared paths.

## Decision: build-time engine selection

Two builds from one module:

```text
go build ./cmd/api            → SQLite engine (default, no tags)
go build -tags pg ./cmd/api   → Postgres engine
```

**Why not runtime selection** (a single binary that reads `DB_DRIVER`):

- `sqlc` generates distinct row types per engine. If both engines' generated code lived in one binary, a single repository implementation could not satisfy either type set — every feature repository would be duplicated or wrapped in mapping adapters. That doubles exactly the code the kit exists to keep small.
- One engine per binary keeps deployments honest: the artifact you tested is the engine you run, with no untested runtime path.
- "Pick the right database per project" maps naturally to build/deploy time in the family's clone-per-product model.

### Build layout

```text
internal/db/gen/
  sqlite_*.go        # //go:build !pg
  pg_*.go            # //go:build pg
internal/platform/db/
  db.go              # common config, DSN handling, readiness plumbing
  store_sqlite.go    # //go:build !pg
  store_pg.go        # //go:build pg
```

- The default build (no tags) compiles the SQLite variant.
- Wrong-engine deployments fail fast: the SQLite build requires `DATABASE_PATH`, the Postgres build requires `DATABASE_URL`. A binary cannot silently point at the wrong engine.
- `platform/db.Engine` is a compiled-in constant (`"sqlite"` / `"postgres"`) used for logging, readiness, and docs.

## Portable SQL subset (shared queries)

Shared queries live in `db/queries/common` and must parse under both engines. Rules:

- Named parameters only: `sqlc.arg(name)` / `sqlc.narg(name)`.
- IDs are generated in Go (UUIDv7); never `gen_random_uuid()` / `randomblob()`.
- Time values are passed from the injected clock; never `now()` / `CURRENT_TIMESTAMP`. Stored UTC.
- Emails are normalized (lowercase) in Go before writes; unique index in both engines.
- Enums as `TEXT` + `CHECK (col IN (…))` in both schemas; booleans map to `bool` in both via sqlc type overrides.
- `RETURNING` and `ON CONFLICT` are allowed (both engines support them).
- Avoid: `ILIKE`, arrays, `jsonb` operators, `INTERVAL` arithmetic, `DISTINCT ON`, `FOR UPDATE`. Dialect-only needs go to `db/queries/pg` or `db/queries/sqlite` and compile only for that engine.
- Keyset pagination on `(created_at, id)` — portable and stable.

Enforcement: generation runs `sqlc` for both engines. A common query that fails either parser fails `make generate` — the discipline enforces itself.

## Generation pipeline (`gokit db generate`)

1. Run `sqlc` twice:

```yaml
# sqlc.yaml (sketch)
version: "2"
sql:
  - engine: "sqlite"
    schema: "db/migrations/sqlite"
    queries: ["db/queries/common", "db/queries/sqlite"]
    gen: { go: { package: "gen", out: ".gen/sqlite" } }
  - engine: "postgresql"
    schema: "db/migrations/pg"
    queries: ["db/queries/common", "db/queries/pg"]
    gen: { go: { package: "gen", out: ".gen/pg", sql_package: "pgx/v5" } }
```

2. Post-process: inject build tags (`//go:build !pg` / `//go:build pg`), engine-prefix file names, copy into `internal/db/gen/`.
3. Drift gate (house pattern): `make generate`, then `git diff --exit-code` in CI — both engines regenerate byte-identically.

Each engine builds against its natural driver (`database/sql` for SQLite, `pgx/v5` for Postgres) because the build tag keeps them apart.

## Runtime differences (contained in `platform/db`)

| Concern | SQLite (default) | Postgres (`-tags pg`) |
| --- | --- | --- |
| Connection | `modernc.org/sqlite` (pure Go), single file | `pgx/v5` pool, sized small (API ≤ 10, worker ≤ 5) |
| Pragmas / settings | WAL, `busy_timeout=5000`, `foreign_keys=ON`, `synchronous=NORMAL` | pool limits, statement cache |
| Write transactions | single writer; `BEGIN IMMEDIATE`; busy-retry | serializable isolation + `40001` retry |
| Reads | concurrent (WAL) | full concurrency |
| Readiness | `SELECT 1` + writable file | `SELECT 1` |
| Backups | Litestream → S3/R2 (continuous) | managed PITR / standard tooling |
| Deployment | api + worker co-located, local SSD | multi-instance behind LB |
| Config key | `DATABASE_PATH` | `DATABASE_URL` |

`platform/db` exposes build-selected `Store` and `InTx(ctx, fn)` with **identical signatures**; feature code passes the store into generated queries and never imports a driver. depguard forbids `modernc.org/sqlite`, `pgx`, and `database/sql` outside `internal/platform/db` and `internal/db/gen`.

## Migrations

Two goose sets: `db/migrations/sqlite/` and `db/migrations/pg/`. Semantically equivalent, dialect-true. The delta is small — typically just column types:

```sql
-- db/migrations/pg/0001_users.sql
CREATE TABLE users (
  id                TEXT PRIMARY KEY,
  email             TEXT NOT NULL UNIQUE,
  status            TEXT NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DELETED')),
  email_verified_at TIMESTAMPTZ,
  created_at        TIMESTAMPTZ NOT NULL,
  updated_at        TIMESTAMPTZ NOT NULL
);

-- db/migrations/sqlite/0001_users.sql
CREATE TABLE users (
  id                TEXT PRIMARY KEY,
  email             TEXT NOT NULL UNIQUE,
  status            TEXT NOT NULL DEFAULT 'ACTIVE'
                    CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DELETED')),
  email_verified_at TEXT,
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL
);
```

A `gokit db check` schema-inventory parity check (compare table/column inventory across engines) runs in CI to catch drift mechanically.

## Testing and CI

- Unit tests run on SQLite by default (temp files, no containers); `miniredis` covers Redis-dependent units.
- Integration and contract suites run as a CI matrix: SQLite lane (no services) and Postgres lane (service container).
- Concurrency-sensitive paths — refresh rotation, reuse detection, last-admin invariants — are explicitly exercised in both lanes.
- Local: `make verify` (SQLite), `make verify-pg` (compose Postgres).

## Data movement between engines

- New project: pick the build; nothing else.
- Existing deployment: stop writes → dump → load. IDs (UUIDv7 strings) and timestamps (UTC) are engine-neutral, so the move is mechanical. An automated `gokit db export/import` helper is an open question (default: later, documented manual steps).

## What doubles vs. what stays single

| Doubles | Stays single |
| --- | --- |
| Migrations — 2 small files per schema change | All feature code: handlers, services, repositories |
| Occasional engine-only query file | Tests — the same suite runs on both engines |
| CI lanes — integration + contract suite run twice | Contract/parity machinery |
| `sqlc` runs twice in `make generate` (automated) | Depguard rules, docs, deployment flows |

## Alternatives considered

- **Runtime engine switch in one binary** — rejected: forces duplicated repositories or per-feature mapping adapters because generated types differ per engine.
- **One engine only** (Postgres alone, or SQLite with no swap) — rejected by the per-project choice requirement.
- **ORM abstraction (GORM) covering both engines** — rejected: reflection, opaque SQL, against the kit's performance and readability goals.
- **One generated package shared by both engines at runtime** — impossible: the generated row types are distinct per engine.

## Risks and tradeoffs

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Portable-subset discipline erodes | Shared queries break one engine | `make generate` fails; rules documented and reviewed |
| Migration sets drift apart | Engines diverge silently | `gokit db check` schema-inventory parity in CI |
| Postgres lane rots if every project ships SQLite | Swap path broken when first needed | CI matrix keeps both lanes green from day one |
| Two concurrency models under one API | Auth/session races in one engine only | Concurrency-sensitive paths tested in both lanes |
| Custom generation step | Maintenance burden | Small, pinned `sqlc` version, drift-gated, owned by `gokit` |
| SQLite single-node ceiling | No horizontal scale on the default build | Documented; the Postgres build is the answer |
| Manual cross-engine data move | Rough edge when swapping existing data | Engine-neutral IDs/timestamps; documented dump/load; helper later |

## Phases

1. **Seam** — subset rules, dual `sqlc` config, build-tag layout, `gokit db generate`, drift gate; SQLite working end-to-end.
2. **SQLite hardening** — pragmas and transaction policy, Litestream + deployment docs, miniredis-based tests.
3. **Postgres engine** — `platform/db` pg variant, CI matrix, schema-inventory check; landed before the kit is declared stable.
4. **Optional** — export/import helper.

## Acceptance criteria

- Default build = SQLite; `-tags pg` = Postgres; each fails fast when its own config key is missing.
- `make generate` is reproducible and drift-gated for both engines.
- Full test + contract suite green in both CI lanes.
- Zero engine imports in `internal/features/**` (depguard proof).
- Schema-inventory parity check green.
- A fresh project reaches the auth flow on SQLite with only Redis as an external service.

## Accepted defaults

All defaults below are accepted (2026-10-10) and become durable decisions via ADRs:

1. **Build tag name** — `pg`.
2. **ID strategy** — UUIDv7.
3. **Export/import helper** — later; manual dump/load steps documented.
4. **Postgres engine timing** — lands before the kit is declared stable (phase 3).
5. **SQLite file location** — `DATABASE_PATH` required in all environments, with a dev example provided.

Durable decisions move into ADRs and normative docs. This proposal remains in `_WIP/` and is not a permanent parallel source of truth.
