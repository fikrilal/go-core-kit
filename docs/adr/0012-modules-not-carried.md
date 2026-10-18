# 0012 — Modules this kit does not carry

- **Status:** Accepted
- **Date:** 2026-10-17
- **Context:** The pinned snapshot from backend-core-kit declares operations for an
  admin surface (`admin.*`) and a merchant-onboarding surface
  (`merchantOnboarding.*`). This kit has carried neither, and the maintainer has
  ruled that it never will: those modules belong to a product that does not exist
  here.

## Decision

go-core-kit carries the auth and me surfaces it implements. The admin and
merchant-onboarding operations declared upstream are **not carried**: no phase
plans them, no follow-up tracks them, and nothing in this repository promises
them.

The allowlist keeps every snapshot operation this kit does not implement — those
eight included. That is deliberate and mechanical: the coverage test compares the
snapshot against the routes and the allowlist in both directions
(`docs/core/project-architecture.md`, ADR-0002), so the entries are what keep
contract drift detectable. Removing them would not drop the modules, it would
disable the gate that notices when upstream changes.

Scaffolding still requires an allowlist entry. Implementing an operation outside
the allowlist already has routes and would collide at the router; the entry is the
record of intent, and the coverage test fails when an implemented route is missing
from both.

## Consequences

- `gokit contract check` keeps reporting the pending count; eight is the expected
  steady state, and a change in it means upstream changed, not that this kit
  planned work.
- The roadmap records the decision instead of a deferred phase: those surfaces are
  out of scope by this ADR, not waiting for a product.
- Phase 9.1 is closed by this decision rather than by implementing the surface:
  every operation this kit carries is declared by the snapshot, and the ones it
  does not carry are accounted for.

## Alternatives considered

- **Implement them.** Rejected: no product need, and the surface is large (RBAC,
  admin reads, audit lists, merchant onboarding) with nothing to test it against.
- **Delete the allowlist entries.** Rejected: it removes the only signal that
  upstream declared operations this kit does not serve, and disables the
  bi-directional coverage check — the failure mode a review caught when this was
  first attempted.
- **A deferred phase in the roadmap.** Rejected: the maintainer's decision is that
  it will never be ported, and a deferred phase reads as future work.
