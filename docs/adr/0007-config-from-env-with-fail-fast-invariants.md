# ADR: Config from Env with Fail-Fast Invariants

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

The platform has many environment knobs (database path or URL, Redis, auth keys, storage, email, push, OTel). Misconfiguration must fail at startup, not at first request. Secrets must never live in git.

## Decision

- Parse env with `caarlos0/env` into typed config structs; defaults live beside the schema.
- `Validate()` runs explicit invariants: all-or-nothing integration groups (email, storage, push), production-required keys, and cross-field constraints.
- Features receive typed sub-config objects (injected), never raw env access.
- `.env` files are dev/test only; `env.example` is kept in sync and never contains real secrets.
- The compiled engine defines which database key is required: `DATABASE_PATH` (SQLite) or `DATABASE_URL` (Postgres).

## Rationale

Typed config is readable, testable, and fails fast; no reflection-heavy config frameworks. The engine-specific key doubles as a wrong-build guard.

## Consequences

Positive: invalid deployments refuse to start; the config surface is discoverable in one place.

Negative: env changes require touching the schema and the example file together.

## Alternatives Considered

- Hand-rolled stdlib parsing — rejected: more code for the same result.
- Viper — rejected: heavy, implicit precedence rules.

## Links / References

- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
