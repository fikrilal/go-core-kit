# ADR: Strict Quality Baseline and Gate Honesty

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

The kit's gold standard is a codebase that stays readable and maintainable as it grows. The sibling harnesses (backendkit, frontendkit, mobilekit) show the family pattern: quality rules are executable, drift is checked mechanically, and gates are trusted because they are known to fail on violations.

Default Go tooling is permissive; "best practice" needs to be a curated, enforced baseline rather than a review convention.

## Decision

1. **Strict lint/format baseline** (landed 2026-10-10): formatting via `gofumpt` + `gci`; linters beyond the standard set — `bodyclose, dupl, errorlint, exhaustive, forbidigo, funlen, gocognit, gocritic, gosec, nilerr, noctx, nolintlint, revive, rowserrcheck, sqlclosecheck, unconvert, unparam, whitespace, wrapcheck` — plus `depguard` import boundaries and `forbidigo` bans (`time.Now` outside `cmd/` and platform adapters, `fmt.Print*`, `print`). Budgets: `funlen` 80 lines / 60 statements, `gocognit` 20, `dupl` 100 tokens.
2. **Harness layers** land with the phases that need them: gokit v0 (verify profiles, doctor, tool pinning) in Phase 1; drift/docs checks in Phase 1; gate honesty in Phase 2; feature-level rules and scaffold enforcement with the first feature; duplication policy and runtime evidence later. See `docs/exec-plans/roadmap.md`.
3. **Gate honesty**: every gate must ship with a meta-test that injects a violation and asserts the gate fails with its expected marker. A check that cannot fail is not a gate.
4. **Baselines are reviewed artifacts**: where findings are baselined, only new findings fail; baseline updates require explicit approval, never a silent rewrite.
5. **Profiles, not step duplication**: `fast`/`full`/`ci` profiles are defined once; `make verify` and CI call the same profile; steps are fail-fast with stable ids and advisory duration budgets.

## Rationale

Rules that matter must fail builds, not reviews. The sibling kits prove this pattern works at template scale: readers can trust the code shape, agents get immediate feedback, and drift cannot accumulate silently. Strictness is cheapest at the start and impossible to retrofit.

## Consequences

Positive: readability and maintainability are enforced, not aspirational; boundary and drift regressions surface mechanically; one command verifies everything.

Negative: contribution friction is higher than default Go; the linter set needs occasional tuning as code lands; tool versions must be pinned and kept in sync (`make tools`).

## Alternatives Considered

- Default `golangci-lint` set only — rejected: misses error-wrapping, context, complexity, and duplication risks the family standard covers.
- Review-only conventions — rejected: does not scale and silently erodes.
- Copying sibling configs wholesale — rejected: their ecosystems differ; only the practices transfer.

## Links / References

- Related ADRs: ADR-0008, ADR-0010
- Related docs: `docs/core/project-stack.md`, `docs/exec-plans/roadmap.md`
