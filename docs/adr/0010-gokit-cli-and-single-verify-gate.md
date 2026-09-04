# ADR: gokit CLI and the Single Verify Gate

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

Each family kit owns a CLI (mobilekit, backendkit, frontendkit). The kit's developer-experience standard requires the simple path to be the default path, and one canonical gate that CI mirrors.

## Decision

- `tools/gokit` provides `scaffold feature`, `scaffold endpoint`, `remove feature`, `db generate`, `doctor`, and verification helpers. Scaffold output must pass `make verify` untouched.
- `make verify` is the single gate: fmt-check + vet + golangci-lint + tests + build. CI runs exactly this target.
- Drift gates (`make generate` + `git diff --exit-code`) cover generated artifacts once codegen lands.
- Boilerplate is generated and removable, not abstracted away.

## Rationale

Scaffolding keeps feature work small without inventing runtime abstractions; a single gate prevents check drift between local runs and CI, and makes "verified" mean one thing.

## Consequences

Positive: new endpoints start green; removing a feature unwires it cleanly; one command to trust.

Negative: `gokit` is maintained with the kit; its templates must track convention changes.

## Alternatives Considered

- Makefile-only — rejected: cannot scaffold or unwire features.
- External generators — rejected: kit-specific conventions belong in-repo.

## Links / References

- Related docs: `AGENTS.md`, `README.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
