# ADR: Stdlib HTTP with Platform Middleware

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

The kit needs an HTTP foundation. Family goals: smallest cost, performance, and a codebase where handlers read top to bottom. The contract requires envelope rendering, RFC7807 problem details, request IDs, idempotency, list-query parsing, and validation errors regardless of framework.

## Decision

Use the stdlib `net/http` (Go 1.22+ method + wildcard patterns) with a small `internal/platform/httpx` layer:

- middleware: request ID → logging → recover → auth → idempotency (opt-in) → list query;
- explicit response helpers: `httpx.Data`, `httpx.List`, `httpx.Created`, `httpx.NoContent`;
- one central problem mapper for typed errors;
- a `validator/v10` decode helper producing `errors[] {field, message}` with `VALIDATION_FAILED`;
- features must not write raw JSON responses (enforced by lint).

`chi` remains the documented fallback if route groups outgrow the stdlib; adopting it requires a superseding ADR.

## Rationale

Zero framework dependency cost; explicit middleware composition; fastest path; nothing hidden between handler and response. The platform layer is needed in any case, and it stays small.

## Consequences

Positive: readable handlers, minimal dependency surface, no framework upgrade treadmill.

Negative: route grouping is more manual than chi; middleware correctness and ordering are our testing responsibility.

## Alternatives Considered

- `chi` — deferred, not rejected: ergonomic subrouters for one dependency; adopt only if stdlib routing becomes a real constraint.
- Echo / Gin — rejected: larger dependency and convention surface than the kit needs.

## Links / References

- Related ADRs: ADR-0008
- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
