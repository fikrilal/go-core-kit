# ADR: Boundaries via golangci-lint and depguard

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

Family kits encode boundaries as executable rules (mobile's architecture lints, backend's dependency-cruiser, frontend's architecture check). Review memory does not scale; the Go kit needs the equivalent.

## Decision

`golangci-lint` (pinned v2.14.0) with `depguard` rules, extended by `forbidigo` as packages land:

- `internal/platform/**` must not import `internal/features/**`;
- `internal/features/**` must not import `cmd/**`;
- database drivers (`modernc.org/sqlite`, `pgx`, `database/sql`) are importable only in `internal/platform/db` and `internal/db/gen`;
- cross-feature imports are default-deny, allowlisted per pair through the target feature's exported surface;
- `internal/shared/**` stays stdlib-only;
- planned additions: no `time.Now()` in service/domain code; no raw JSON response writes in features.

CI runs the same gate as `make verify`.

## Rationale

Rules that matter must fail builds, not reviews. The rules stay few, explicit, and searchable, and they evolve as packages appear.

## Consequences

Positive: architecture regressions surface mechanically; agent and human contributors get immediate feedback.

Negative: the lint config is another maintained artifact; rule changes must be deliberate when architecture changes.

## Alternatives Considered

- `go-arch-lint` / `import-boss` — viable, but add a second tool; depguard ships with golangci-lint.
- Custom analyzers — deferred; revisit if path-scoped rules (for example, raw JSON writes in features) cannot be expressed with forbidigo.

## Links / References

- Related ADRs: ADR-0003
- Related docs: `docs/core/project-architecture.md`, `AGENTS.md`
