# Roadmap

- Status: living document — update as phases and tasks land
- Updated: 2026-10-10
- Scope: high-level phases, task tracking, and exit gates. Details live in per-task execution plans.

## How To Use

- This roadmap is the index; it never replaces an execution plan.
- Every task gets a plan from `_template.md` stored in `docs/exec-plans/active/` before work starts; completed plans move to `docs/exec-plans/completed/` with verification evidence.
- Update checkboxes and "Current Focus" as tasks complete. A task is done only when its gate actually ran.
- `make verify` is the standing gate for every task; phases add gates (contract suite, drift checks, engine lanes).
- Decisions that change a baseline require an ADR first (see `docs/adr/README.md`).

## Harness

The harness keeps quality golden. Layers, in landing order (practices ported from backendkit, frontendkit, and mobilekit):

1. **Strict lint/format baseline** (landed, 1.0): `gofumpt` + `gci` formatting; curated strict linters (`dupl`, `errorlint`, `exhaustive`, `forbidigo`, `funlen`, `gocognit`, `gocritic`, `gosec`, `nilerr`, `noctx`, `nolintlint`, `revive`, `wrapcheck`, ...); `depguard` boundaries; `forbidigo` bans (`time.Now` outside adapters, `fmt.Print*`, `print`). Probes confirmed the gates fail on injected violations and exclusions work.
2. **gokit v0** (1.11): typed command layer, `verify --profile fast|full|ci`, `doctor`, tool pinning via `make tools`. `make verify` delegates to the full profile; CI calls the same profile.
3. **Drift & docs checks** (1.12): `env.example` ↔ config schema, project map (AGENTS.md tree ↔ actual dirs), markdown link integrity, ADR index completeness.
4. **Gate honesty** (2.5): every gate ships with a meta-test that injects a violation and asserts the gate fails with the expected marker — a check that cannot fail is not a gate.
5. **Feature-level enforcement** (3.9, 6.x): forbidigo rules for feature code, scaffold output smoke-tested against `make verify`, `remove feature` unwiring.
6. **Duplication policy + evidence** (later): blocking core duplication profile with reviewed allowlists; oracles/evidence once runtime flows exist.

Rules: findings gate on **new vs baseline**, and updating a baseline requires explicit approval (never silent). Profiles are fail-fast with per-step ids and advisory duration budgets.

## Current Focus

**Phase 1 — Platform core**: 1.0–1.8 landed (strict baseline, config, logging, httpx + validation + list query + idempotency, probes, Redis); next up 1.9 (DB seam), with harness v0 (1.11–1.12) alongside.

## Phase 0 — Foundation (complete, 2026-10-10)

- [x] Toolchain: Go 1.27.2 + golangci-lint v2.14.0 (user-local; lint version pinned in CI)
- [x] Module scaffold: `go.mod`, `internal/platform/version`
- [x] Build/lint/CI foundation: `make verify` gate, `.golangci.yml` (depguard boundaries), CI workflow mirroring the gate
- [x] Docs taxonomy, `AGENTS.md`, ADR 0001–0010, both `_WIP` proposals

Evidence: commits `558f875` (docs) and `6b068e6` (scaffold); `make verify` green.

## Phase 1 — Platform core

Objective: the platform layer every feature consumes — config, logging, HTTP behavior, validation, list query, idempotency, Redis, the DB seam (SQLite engine), health, and a booting `cmd/api`.

- [x] 1.0 Strict quality baseline: `gofumpt` + `gci` formatters, curated strict linters, `depguard` + `forbidigo` posture, Makefile/`make tools` integration (landed 2026-10-10; violation probes confirmed gates fail)
- [x] 1.1 Config: env schema, typed structs, defaults, invariant validation, `env.example` (`internal/platform/config`) — landed 2026-10-10; 7 tests green
- [x] 1.2 Logging: `slog` JSON handler, redaction list, request-bound fields (`internal/platform/logging`) — landed 2026-10-10; 5 tests green
- [x] 1.3 HTTP foundation: router + middleware (request id, logging, recover), envelope helpers, central problem mapper, typed error codes (`internal/platform/httpx`) — landed 2026-10-10; 13 tests green
- [x] 1.4 Input validation: decode + validate helper → `errors[] {field, message}` (`httpx` + validator/v10) — landed 2026-10-10; 8 new tests green
- [x] 1.5 Health/ready handlers (`internal/platform/health`; envelope-skipped) — landed 2026-10-10; 4 tests green; access-log exclusion via `httpx.SkipAccessLog`
- [x] 1.6 Redis platform: client, key conventions, readiness wiring (`internal/platform/redis`) — landed 2026-10-10; 3 tests green (miniredis, no Docker); driver confined by depguard
- [x] 1.7 List query: cursor/limit/sort/filter parser with per-endpoint allowlists (`httpx`) — landed 2026-10-10; 12 tests green
- [x] 1.8 Idempotency middleware: Redis lock + cached replay + `Idempotency-Replayed` — landed 2026-10-10; 7 tests green; missing key passes through, only 2xx cached, Redis failures fail closed
- [ ] 1.9 DB seam: sqlc dual-engine config, portable subset rules, goose runner, SQLite engine (`internal/platform/db`), minimal `gokit db generate`, drift gate
- [ ] 1.10 API bootstrap: `cmd/api` composition root with graceful shutdown serving `/health` + `/ready`
- [ ] 1.11 Harness skeleton (`gokit` v0): typed command layer + `verify --profile fast|full|ci` + `doctor` + tool pinning; `make verify` delegates to the full profile
- [ ] 1.12 Drift & docs checks: `env.example` ↔ config schema, project map (AGENTS.md tree ↔ dirs), markdown link integrity, ADR index completeness

Exit gate: `make verify` green; `cmd/api` boots; `X-Request-Id` echoed on `/health`, `/ready`; envelope, problem, and validation paths covered by platform tests; generated-artifact drift gate works.

## Phase 2 — Contract pinning

Objective: the pinned contract and the machinery that proves client parity.

- [ ] 2.1 Pin backend snapshot: `contracts/backend.openapi.yaml` + `provenance.json` (source commit + SHA-256)
- [ ] 2.2 Generate request/response types (`oapi-codegen`, types only) + `make generate` + drift gate
- [ ] 2.3 Contract suite harness: live-response validation (`kin-openapi`) + header/status/error-code assertions
- [ ] 2.4 Operation allowlist + coverage check inside `make verify`; `vacuum` spec lint
- [ ] 2.5 Gate-honesty meta-test: inject a stale snapshot and a forbidden import; both gates must fail with their expected markers

Exit gate: harness runs against the booted API; implemented-vs-allowlisted operation inventory is enforced.

## Phase 3 — Auth slice (clean tier)

Objective: the full auth surface, contract-green.

- [ ] 3.1 Schema + migrations: users, credentials, sessions, refresh tokens (sqlite set first)
- [ ] 3.2 Password register + login (argon2id, dummy-hash equalization, rate limits)
- [ ] 3.3 Sessions: refresh rotation, reuse detection, logout
- [ ] 3.4 Access tokens: EdDSA keyring, JWKS endpoint
- [ ] 3.5 Change password
- [ ] 3.6 Email verification + resend *(needs 5.2)*
- [ ] 3.7 Password reset request/confirm *(needs 5.2)*
- [ ] 3.8 OIDC exchange
- [ ] 3.9 `forbidigo` rules land with the first feature (no `time.Now()` in services, no raw JSON writes)

Exit gate: contract suite green for all auth operations; rotation/reuse parity assertions pass.

## Phase 4 — Me slice

- [ ] 4.1 `/v1/me` GET/PATCH
- [ ] 4.2 Sessions list/revoke
- [ ] 4.3 Push token upsert/revoke
- [ ] 4.4 Profile image flow (presign → complete → url → clear)
- [ ] 4.5 Account deletion request/cancel

Exit gate: contract suite green for users/me operations.

## Phase 5 — Worker + jobs

- [ ] 5.1 Worker bootstrap: asynq server, retries/backoff, OTel propagation, `/health` + `/ready`
- [ ] 5.2 Email jobs + Resend adapter (unblocks 3.6/3.7)
- [ ] 5.3 Push jobs + FCM adapter
- [ ] 5.4 Account deletion jobs (grace period)

Exit gate: email verification/reset flows complete end-to-end.

## Phase 6 — gokit CLI + guides

- [ ] 6.1 `scaffold feature` / `scaffold endpoint` (output passes `make verify` untouched)
- [ ] 6.2 `remove feature` (unwires routes/DI/tests cleanly)
- [ ] 6.3 `doctor` + completion of `db generate`
- [ ] 6.4 Guides: adding-a-feature, adding-an-endpoint, config, jobs (`docs/guide/`)
- [ ] 6.5 Scaffold smoke: generated output verified by `make verify` in CI

Exit gate: a fresh clone can scaffold a working endpoint in one command.

## Phase 7 — Postgres engine + dual-lane CI

- [ ] 7.1 `platform/db` pg variant (pgx pool, serializable retry) + pg migrations + portable subset audit
- [ ] 7.2 CI matrix: integration + contract suites on both engines; schema-inventory parity check
- [ ] 7.3 `make verify-pg`

Exit gate: both lanes green — required before declaring the kit stable (ADR-0004).

## Phase 8 — Admin + RBAC (tier 2, optional)

Deferred until a product needs it; the contract operations (`/v1/admin/*`) are defined upstream.

## Phase 9 — Parity + budget closure

- [ ] 9.1 Full client-facing operation coverage (allowlist empty)
- [ ] 9.2 Performance/cost baseline recorded (image size, idle RSS, p95 on reference endpoints)
- [ ] 9.3 `docs/standards/*` completed; residual-risk log

Exit gate: acceptance criteria of both `_WIP` proposals satisfied.

## Cross-Cutting Follow-Ups

- [x] Strict lint/format baseline + `make tools` for golangci-lint (landed with 1.0)
- [ ] Extend `make tools` pins for `sqlc`, `goose`, `oapi-codegen`, `vacuum` as they land
- [ ] Duplication policy: blocking core profile with reviewed allowlists (once features exist)
- [ ] Secrets scan gate (pattern-based, `git ls-files` scoped) in CI
- [ ] Litestream backup + single-box deployment notes (`docs/engineering/`)
- [ ] Export/import helper for cross-engine data moves (optional, later)
- [ ] Evidence/oracles scaffolding once runtime flows exist

## Open Items Kept Visible

- Postgres engine must land before "stable" (ADR-0004 / dual-engine proposal)
- Operation allowlist must shrink to empty for the client-facing surface
- Performance budget recorded once, on the smallest target instance
