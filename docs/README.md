# Go Core Kit — Documentation

go-core-kit is a production-grade Go backend template that serves the same wire contract as `backend-core-kit`. These docs are the source of truth for architecture, standards, and workflow; code follows the docs.

## Navigation

- Core
  - `docs/core/project-architecture.md`
  - `docs/core/project-stack.md`
- Standards (normative)
  - `docs/standards/README.md` — index; individual standards land with the platform core
- ADRs (decision log)
  - `docs/adr/README.md`
- Guides
  - `docs/guide/adding-a-feature.md`
  - `docs/guide/adding-an-endpoint.md`
  - `docs/guide/config.md`
  - `docs/guide/jobs.md`
- Execution plans
  - `docs/exec-plans/README.md`
  - `docs/exec-plans/roadmap.md` — high-level phases and task tracking
- Contract
  - `contracts/` — pinned OpenAPI snapshot + provenance + generated types (lands in the contract phase)
- Proposals (historical, not source of truth)
  - `_WIP/go-core-kit-architecture-proposal.md`
  - `_WIP/go-core-kit-dual-engine-data-layer-proposal.md`
  - `_WIP/go-core-kit-readability-proposal.md` — draft; implementation taste against mobile-core-kit

## Doc Conventions

- "Standards" are normative. Deviating requires an ADR and a documented exception.
- Prefer small, composable standards. Avoid one-off patterns that don't scale to multiple projects.
- Keep docs implementation-aware (file paths, config keys, behaviors) without code dumps.
