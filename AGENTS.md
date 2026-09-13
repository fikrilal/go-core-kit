# Repository Guidelines

## Operating Contract

- Keep changes small, focused, and reversible.
- Simplicity first: the minimum code that solves the problem. No speculative abstractions or flexibility that was not requested. If 200 lines could be 50, rewrite it.
- Comments explain non-obvious decisions only. Exported API keeps a one-line doc comment (lint-enforced); nothing else restates the code.
- Prefer the existing architecture, helpers, scripts, and standards over new patterns.
- If behavior, architecture, standards, or public contracts change, update the relevant docs and add an ADR when a decision changes a baseline.
- Do not commit or push unless the user explicitly asks. Use Conventional Commits when committing.
- The wire contract is owned by backend-core-kit. This kit follows it; it never invents or changes contract surface.

## Source of Truth

Start with `docs/README.md`, then follow the topic-specific source:

- Architecture: `docs/core/project-architecture.md`
- Stack: `docs/core/project-stack.md`
- Standards (normative): `docs/standards/README.md`
- ADRs: `docs/adr/README.md`
- Execution plans: `docs/exec-plans/README.md`
- Contract: `contracts/` (pinned backend-core-kit OpenAPI snapshot + generated types)
- Proposals (historical, not source of truth): `_WIP/`

## Hard Rules

- Successful JSON responses use the envelope `{ data, meta? }`; lists return `data` plus `{ nextCursor?, limit, hasMore }` meta.
- Errors are RFC7807 problem details with a stable `code`, `traceId` (= `X-Request-Id`), and optional `errors[]`.
- `X-Request-Id` is accepted and echoed on every response. Retryable writes honor `Idempotency-Key` and set `Idempotency-Replayed: true` on replay.
- Error codes are typed constants; raw production error-code strings are rejected by construction.
- Features must not write raw JSON responses — use `httpx` helpers. `encoding/json` response writes and direct `w.Write` are forbidden in `internal/features/**`.
- Database drivers (`modernc.org/sqlite`, `pgx`, `database/sql`) are importable only inside `internal/platform/db` and `internal/db/gen`.
- SQL lives in repository code and `db/queries`; handlers and services never build queries.
- Time in service/domain code comes from an injected clock; no direct `time.Now()`.
- Repository classes/structs are the persistence boundary.
- Secrets never go in git. Use env/runtime injection.
- Contract gates: implemented operations ⊆ pinned snapshot; generated artifacts are regenerated, committed, and drift-checked.

## Architecture Map

```text
cmd/api/            HTTP process bootstrap (composition root)
cmd/worker/         background jobs bootstrap (composition root)
internal/platform/  cross-cutting: config, httpx, auth, db, redis, queue, ...
internal/features/  vertical slices (progressive tiers)
internal/shared/    framework-free cross-feature contracts and utilities
db/migrations/      goose migrations per engine (sqlite/, pg/)
db/queries/         sqlc input; portable subset in common/
internal/db/gen/    generated query code (build-tag selected engine)
contracts/          pinned OpenAPI snapshot + provenance + generated types
tools/gokit/        scaffold / verify / doctor CLI
docs/               standards, ADRs, exec plans, and engineering notes
```

Dependency direction:

- `internal/platform/*` must not import `internal/features/*`.
- `internal/features/*` must not import `cmd/*`.
- Cross-feature imports only through a feature's exported `Deps`/`Register` surface, allowlisted per pair, acyclic.
- `internal/shared/*` stays stdlib-only and framework-free.

## Verification

Use the repo scripts; do not claim a check passed unless it was actually run.

- `make verify` — full gate via `gokit verify --profile full`: knowledge checks, format, vet, lint (boundary rules), tests, builds, and generated-code drift
- Once platform code lands: `make generate` + `git diff --exit-code` (drift gate for sqlc output, contract types), `make verify-pg` (Postgres engine lane).

Record exact verification commands and outcomes for auth/session, queue, idempotency, and contract-surface changes.

## Delivery Notes

- Summarize changed files, behavioral impact, and verification evidence.
- Call out skipped checks and why.
- Call out residual risk, especially for auth, contract surface, idempotency, and CI.
- Leave unrelated dirty worktree changes untouched.
- Leave completed work uncommitted by default; wait for an explicit user request before creating commits.
