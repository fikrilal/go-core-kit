# ADR: Redis Roles — Queue, Idempotency, Rate Limits

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

Redis appeared "everywhere" during review; its necessity and footprint were questioned. In this kit it is not a cache layer sprinkled through feature code — it is load-bearing for three platform concerns.

## Decision

Redis (`go-redis/v9`) is required and has exactly three roles:

1. **Queue** — `asynq` (Redis-backed) for email, push, and account-deletion jobs.
2. **Idempotency** — `Idempotency-Key` lock plus cached response replay, with `Idempotency-Replayed: true`.
3. **Rate limiting / lockout** — login, password-reset, and email-resend protection.

Rules:

- every key carries a TTL;
- job payloads carry IDs, not blobs (handlers fetch from the database);
- `/ready` includes Redis;
- features never import Redis directly — platform helpers only (lint-enforced);
- the SQLite and Postgres builds use Redis identically.

## Rationale

One small service (~10–30 MB) replaces three custom Postgres implementations and matches backend-core-kit's infrastructure. Idempotency and rate limiting are contract and security behaviors, not caching conveniences; running them off the primary database keeps brute-force load away from it.

## Consequences

Positive: simple, standard implementations; identical in both engine builds; near-zero resource cost.

Negative: one more service in every deployment; Redis availability affects readiness and write replay.

## Alternatives Considered

- `river` (Postgres queue) plus Postgres-backed idempotency and rate limiting — rejected: three custom implementations, more code, and more database load; brute-force protection would hit the database.
- In-memory only — rejected: no durability across restarts, breaks multi-instance.

## Links / References

- Related ADRs: ADR-0004
- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
