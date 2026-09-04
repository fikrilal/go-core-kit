# ADR: Modular Monolith and Progressive Feature Tiers

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

go-core-kit is a new, empty repository in the core-kit family. It must serve the same wire contract as backend-core-kit and follow the family's house style: simplicity first, optional use cases, and a heavy shared core that keeps feature work small.

backend-core-kit learned (ADR 0018) that mandatory `domain/app/infra` layers make simple endpoint work expensive, and mobile-core-kit (ADR 0009) uses graduated feature shapes with explicit promotion triggers. A greenfield Go kit can adopt the progressive model from day one.

## Decision

Adopt a **modular monolith** with two processes and a progressive feature architecture.

Top-level layout:

```text
cmd/api/            HTTP process bootstrap (composition root)
cmd/worker/         background jobs bootstrap (composition root)
internal/platform/  cross-cutting concerns (config, httpx, auth, db, queue, ...)
internal/features/  vertical slices
internal/shared/    framework-free cross-feature contracts
```

Feature tiers — start with the smallest shape that fits:

| Tier | Use when | Shape |
| --- | --- | --- |
| Endpoint slice | Simple CRUD, settings, read/list endpoints, one repository | handler, optional service, repository, optional errors, colocated tests |
| Capability slice | Multiple related endpoints share behavior, policies, or persistence | capability folders under the feature with shared repository/errors |
| Clean slice | Auth, deletion, queues, transaction-heavy invariants, multiple adapters | explicit `domain` / `app` / `ports` / `infra` packages |

Promotion triggers (any one): pure rules needing independent tests; multiple adapters for one use case; external side effects through ports; transactions spanning aggregates; queue retries/idempotency/eventual consistency affecting correctness; auth/session/security-sensitive changes; extraction expected; complex repository behavior.

Use cases are optional: add a service only when the operation has logic worth isolating — never a pure pass-through.

## Rationale

Proportional to risk. Simple endpoints stay few files; complex slices still get full boundaries. Matches the family's tested conventions and keeps features readable — the kit's gold standard.

## Consequences

Positive: fewer files per simple feature; features grow without rewrites; navigation by capability; composition stays explicit.

Negative: promotion triggers are reviewed, not lint-enforced; some features will intentionally stay "flat".

## Alternatives Considered

- Mandatory clean-architecture layers for every feature — rejected: ceremony without corresponding risk.
- Flat single `src/` tree — rejected: loses platform/feature ownership and process boundaries.
- DI container/framework (fx, wire) — rejected: magic and indirection against the kit's readability goals.

## Links / References

- Related ADRs: ADR-0003, ADR-0008
- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
