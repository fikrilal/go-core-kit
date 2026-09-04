# ADR: Wire-Contract Parity with backend-core-kit

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

frontend-core-kit and mobile-core-kit clients already speak backend-core-kit's wire contract: envelope `{data, meta?}`, RFC7807 problem details with stable codes and `traceId`, `X-Request-Id`, idempotent writes, cursor lists, and first-party session tokens with JWKS. A Go implementation must be indistinguishable to clients — "no extra tuning" is the requirement.

Without a mechanical check, two codebases drift.

## Decision

Treat backend-core-kit's committed OpenAPI snapshot as the contract and prove parity mechanically:

1. Pin the snapshot into `contracts/backend.openapi.yaml` with `contracts/provenance.json` (source commit + SHA-256).
2. Generate request/response Go types from the snapshot (`oapi-codegen`, types only). Handlers are hand-written; generated code is committed and drift-gated.
3. Run a contract suite that validates live responses against the pinned snapshot (`kin-openapi`) and asserts headers, status codes, `x-error-codes` membership, and token rotation/reuse behavior.
4. Maintain an explicit allowlist of not-yet-implemented operations; CI fails if an implemented operation is missing or unvalidated.
5. Lint the snapshot with `vacuum`.

## Rationale

Schema parity alone is not enough — clients also observe headers, statuses, and error codes. Pinning plus live-response validation makes "clients need no tuning" a test result instead of a claim. Contract changes originate upstream and arrive as deliberate pull steps.

## Consequences

Positive: drift fails CI; generated types remove hand mapping; the implemented surface is explicit.

Negative: snapshot updates are coordinated work; Go-side contract experiments require changing backend-core-kit first.

## Alternatives Considered

- Hand-written types mirrored from the spec — rejected: drift-prone.
- Code-first OpenAPI generation in Go — rejected: two sources of truth.
- Full server-interface generation (`oapi-codegen` strict server) — rejected: rigid, generator-shaped handlers.

## Links / References

- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
