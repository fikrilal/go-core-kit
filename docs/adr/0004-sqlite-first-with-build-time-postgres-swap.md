# ADR: SQLite First with Build-Time Postgres Swap

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

Resource review: Postgres costs 40–80 MB baseline plus a service to run, tune, and back up; SQLite is in-process (~5–20 MB, no service) and sufficient for auth-scale write loads. Some products will need multi-instance scale, and the kit targets multiple projects that each should pick the right engine.

## Decision

SQLite is the default engine; Postgres is an opt-in build. The engine is selected at **build time**:

```text
go build ./cmd/api            → SQLite (modernc.org/sqlite, pure Go)
go build -tags pg ./cmd/api   → Postgres (pgx/v5)
```

- Runtime switching is rejected: generated row types differ per engine, so one binary containing both forces duplicated repositories or mapping adapters per feature.
- Wrong-engine deployments fail fast: the SQLite build requires `DATABASE_PATH`; the Postgres build requires `DATABASE_URL`.
- `platform/db` owns engine differences behind identical signatures: WAL + `busy_timeout` + single-writer `BEGIN IMMEDIATE`/busy-retry vs. pool + serializable/`40001` retry.
- The Postgres engine lands before the kit is declared stable, with a CI matrix running integration and contract lanes on both engines.

## Rationale

Cheapest possible deployment by default (one small box, no database server) with a contained, pre-designed scale path. Duplication is limited to migrations and rare dialect-specific queries; feature code and tests stay single-sourced.

## Consequences

Positive: no Docker needed for the DB in dev/tests; smallest footprint; per-project engine choice; engine-neutral IDs (UUIDv7) and UTC timestamps make data moves mechanical.

Negative: SQLite is single-node (api + worker co-located, local SSD); two migration sets; portable-subset discipline for shared queries; cross-engine data migration is manual in v1 (a helper may come later).

## Alternatives Considered

- Runtime driver switch in one binary — rejected: duplicate repositories per feature.
- Postgres only — rejected: adds a service and cost to every project.
- SQLite only — rejected: no scale path for larger products.
- ORM supporting both engines — rejected: reflection and opaque SQL against kit goals.

## Links / References

- Related ADRs: ADR-0005, ADR-0006
- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-dual-engine-data-layer-proposal.md`
