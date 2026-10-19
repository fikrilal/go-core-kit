# x1 — Duplication policy

## Objective

Turn duplication from a linter setting into a stated policy: a threshold, a
definition of what counts as a legitimate near-duplicate, and an explicit,
reviewed way to allow one. Today `dupl` runs at threshold 100 inside the lint step
with no standard behind it, and the two times it fired while landing the pg lane
were resolved ad hoc.

## Deliverables

1. `docs/standards/duplication.md` — the normative page: what the rule catches,
   the threshold and why, the shapes that are *not* duplication (two ports over
   different generated row types, two loaders over different tables), how an
   exception is requested and recorded, and what a reviewer should check.
2. `tools/gokit` gains a **probe** for the rule, because ADR-0011 says a check that
   cannot fail is not a gate: inject a duplicated function into a fixture package
   and assert the lint gate fails with a dupl marker. The `gates honesty` scenario
   list grows by one.
3. The existing near-duplicates are resolved or recorded: the token loaders
   (`TokenByHash(kind, …)` is the resolved case), the me/auth limiters, and the
   contract-test seeders. Each either collapses into one implementation or appears
   in the standard's reviewed-allowlist section with its reason.
4. The threshold is chosen deliberately in `.golangci.yml` with a comment pointing
   at the standard, so changing it is a decision rather than a tweak.

## Acceptance checks

1. A deliberately duplicated function fails `make lint` (proved by the new honesty
   probe, which runs in the full profile).
2. The standard names every current exception and the code carries no bare
   `//nolint:dupl` without a reason matching the standard.
3. `make verify` and `make verify-pg` stay green.

## Outcome — landed 2026-10-17

- `docs/standards/duplication.md` states the rule and, more usefully, what is *not*
  duplication: two ports over different generated row types, two adapters over
  different providers, and fixtures that mirror a table. The test is whether there
  is one decision or two that currently agree.
- The exceptions table is **empty**, and that is accurate: no `//nolint:dupl` exists
  in the tree. The token loaders merged into `TokenByHash(kind, …)`, the limiters
  share one implementation, and the contract-test seeders collapsed into
  `seedAuthToken` when the pg lane forced the question.
- The threshold stays at 100 in `.golangci.yml`, now with the standard behind it
  rather than a bare number.
- **The gate is proven.** `gokit gates honesty` grew a seventh scenario: it writes
  two deliberately identical, threshold-crossing functions into a temporary package
  inside the module, runs the lint gate over it, and asserts the failure carries the
  dupl marker. The first fixture was too short to trip the rule and the probe failed
  — which is exactly the check working as intended.

Verification: `golangci-lint run` 0 issues, `make verify` full profile passed,
`make verify-pg` passed, and `gokit gates honesty` reports every probed gate failing
as designed.
