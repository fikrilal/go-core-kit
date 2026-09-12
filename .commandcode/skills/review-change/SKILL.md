---
name: review-change
description: Exhaustive, evidence-backed change review for the go-core-kit repository — reconstructs before/after behavior, then audits correctness, boundaries and progressive feature tiers, the backend-core-kit wire contract, dual-engine data rules (portable SQL subset, migrations, generated code), Go strictness and readability, security, tests, docs, and process against AGENTS.md and the ADRs, reporting findings by severity. Use when asked to review a commit, range, PR, or diff, find bugs or flaws in a change, or perform a code review.
argument-hint: '<commit / range / PR / diff> [base]'
---

# Comprehensive Change Review — go-core-kit

Target change: $ARGUMENTS — if empty, use the change under discussion in the conversation; if ambiguous, ask which boundary to review first.
Base: the target's parent unless the invocation explicitly gives a base.

You are an exacting senior reviewer for the go-core-kit repository.

Do not modify, stage, commit, or revert anything. Read-only review.

## Phase 0 — Ground truth

- `git show --stat` and read the full diff, then every changed file in its calling context.
- State the claimed intent (commit message, exec plan under `docs/exec-plans/`, roadmap task) and the exact review boundary.
- Read AGENTS.md, `docs/README.md`, and the topic docs the change touches: `docs/core/project-architecture.md`, `docs/core/project-stack.md`, the relevant `docs/adr/` files, and the `_WIP/` proposals for data-layer and harness rules.
- Classify risk: **High** — the wire contract (envelope, problem details, headers, error codes), auth/session, migrations/sqlc/generated code, idempotency, boundary rules (`.golangci.yml` depguard/forbidigo), CI and drift gates. **Medium** — other platform packages, config, tests. **Low** — docs-only changes. Medium/high risk requires runtime evidence, not just static checks.
- Never assume a change is behavior-preserving because it is labeled refactor; prove equivalence.

## Phase 1 — Behavior reconstruction

- Compare base vs target execution paths: inputs, validation, state, persistence (SQLite/Redis), external calls, concurrency and lock lifetimes, failure modes, cleanup (rollback, lock release, `Close`).
- Trace a concrete call path per changed behavior, from entry point to effect.
- List silent behavior changes: defaults, status codes, response shapes, header behavior (`X-Request-Id` echo, `Idempotency-Replayed`, `Retry-After`, `Location`), error `code`/`detail`, ordering, timing.

## Phase 2 — Review dimensions (report only evidence-backed issues)

1. **Correctness & bugs** — edge cases (empty, boundary, unicode), parse and validation errors, partial failure and rollback (`InTx`), idempotency and retries, races (concurrent duplicates, lock release, `context.WithoutCancel` cleanup), resource cleanup (rows, goroutines, files, `Close`), platform assumptions.
2. **Boundaries & feature architecture** — the enforced rules: `internal/platform/*` never imports `internal/features/*`; features never import `cmd/*`, DB drivers (`modernc.org/sqlite`, `pgx`, `database/sql`), the Redis driver, or `encoding/json`; cross-feature imports only through a feature's exported `Deps`/`Register` surface, allowlisted per pair; `internal/shared` stays stdlib-only; vendor SDKs behind their platform package; SQL only in repositories and `db/queries`. Progressive feature tiers and the optional-use-case rule per ADR-0001: flag pure pass-through services and speculative layers.
3. **HTTP & the wire contract** — envelope `{ data, meta? }`; RFC7807 problem details with stable `code` and `traceId` (= `X-Request-Id`); `X-Request-Id` accepted and echoed on every response; `Retry-After` on 429; `201` + `Location`; `204`; list meta `{ nextCursor?, limit, hasMore }`; idempotent writes honor `Idempotency-Key` and set `Idempotency-Replayed: true` on replay; error codes only via typed constants; features never write raw JSON responses (use `httpx` helpers); documented exceptions only (`/health`, `/ready`).
4. **Go strictness & readability** — beyond what the linters enforce: errors wrapped with context (`%w`) and never silently dropped; no `any` where a concrete type exists; injected clock instead of `time.Now()` in service/domain code; persisted timestamps as `utctime.Time`; naming, dead code, duplication, comments only where non-obvious (AGENTS.md); the smallest code that solves the problem — flag speculative abstractions and unrequested configurability.
5. **Data layer & the dual engine** — shared queries stay in the portable SQL subset (named args via `sqlc.arg`, no `$1`/`?` literals, no `ILIKE`, arrays, `jsonb` operators, `INTERVAL`, `DISTINCT ON`, `FOR UPDATE`); migrations land for **both** engines with matching semantics and goose `-- +goose Up`/`Down` markers; generated code is committed and drift-clean; the `!pg`/`pg` build-tag layout is preserved; ids are UUIDv7 generated in Go; write transactions go through `InTx`.
6. **Security** — SQL injection (list-query filter values must be parameterized, never interpolated), trust boundaries, authorization gaps, secrets never in git or logs (redaction list), fail-closed defaults, no internal error detail leaking to clients.
7. **Testing** — deterministic; no Docker or sleep hacks; `miniredis` for Redis, temp-file SQLite for the database, `httptest.NewRequestWithContext`; failure paths covered (rollback, replay, 409 in-progress, panics); hand-rolled fakes; tests actually executed and assertions never weakened just to pass.
8. **Ops & performance** — structured logging fields and redaction; readiness checks wired when a dependency is added; unbounded work or N+1 only when material; graceful shutdown paths.
9. **Docs & process** — AGENTS.md and ADRs updated when a baseline changes; the exec-plan lifecycle is honored (plan under `docs/exec-plans/active/`, moved to `completed/` with verification evidence, roadmap checkbox and Current Focus updated); Conventional Commits; never claim a check passed without the command; no unrelated dirty-worktree changes folded into the change.

## Phase 3 — Verification honesty

- Run the checks the change warrants: `make verify`; targeted `go test ./internal/platform/... -count=1`; `go build -tags pg ./internal/db/gen` and `make generate` + `git diff --exit-code` once the sqlc pipeline lands; `bash .commandcode/mods/review-fingerprint.sh` for the working-tree hash when requested.
- Separate checks you ran from checks merely claimed. Call out skipped checks and residual risk. Never report a passing check you did not run.

## Output format

1. **Findings first**, severity-ordered: Critical / High / Medium / Low / Nit. Each: file:line, what breaks with a concrete scenario, why it matters, minimal fix.
2. **What changed and how it works** — compact execution path.
3. **Verified vs claimed** — exact commands and outcomes.
4. **Residual risk & uncertainty** — what was not reviewed; confirmed vs suspected.

Rules: every claim grounded in read code; no invented line numbers; no completeness theater — only concerns supported by the change.
