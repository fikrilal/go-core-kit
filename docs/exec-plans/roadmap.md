# Roadmap

- Status: living document — update as phases and tasks land
- Updated: 2026-10-11
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

**Phase 5: 5.1–5.4 landed.** Exit gate still needs **3.6/3.7** (email verification and password reset end-to-end), both unblocked by 5.2.

Readability note 01 has landed.

- [x] R1 httpx taste — `completed/r1-httpx-taste.md`
- [x] R2 httpx problem details (B3 shipped, B1 blocked) — `completed/r2-httpx-problem-details.md`
- [x] R3 httpx idempotency caller scope (B2) — `completed/r3-httpx-idempotency-scope.md`

Readability note 02 is in two plans.

- [x] N2 config order and redact limit — `completed/n2-config-redact.md`
- [x] N2 health probes — `completed/n2-health-probes.md`

Readability note 03 comments and the three auth bugs have landed. Verification stays EdDSA only.

- [x] N3 auth comments — `completed/n3-auth-comments.md`

Readability note 04 starts with comments. The zero-time write and the schema name comparison have landed. The Postgres CI matrix stays task 7.2.

- [x] N4 db comments — `completed/n4-db-comments.md`
- [x] N4 zero time writes NULL — `completed/n4-zero-time-null.md`
- [x] N4 schema name inventory — `completed/n4-schema-inventory.md`

Readability notes 05 through 08 are applied.

- [x] N5 redis and password comments — `completed/n5-redis-password-comments.md`
- [x] N6 auth feature taste — `completed/n6-auth-feature-taste.md`
- [x] N7 api boot comments — `completed/n7-api-boot-comments.md`
- [x] N8 contract comments and feature map — `completed/n8-contract-comments.md`

Held bugs from notes 02 and 03 are fixed. Method mismatch stays 404: the pinned snapshot has no 405 response and no method-not-allowed code.

- [x] N2 nested redaction — `completed/n2-nested-redact.md`
- [x] N2 version claim — `completed/n2-version-claim.md`
- [x] N3 reject non-EdDSA keys — `completed/n3-reject-non-eddsa.md`
- [x] N3 trim issuer and audience — `completed/n3-trim-issuer.md`
- [x] N3 stack sentence is EdDSA only — `completed/n3-stack-eddsa.md`

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
- [x] 1.9 DB seam: sqlc dual-engine config, portable subset rules, goose runner, SQLite engine (`internal/platform/db`), minimal `gokit db generate`, drift gate — landed 2026-10-10; 1.9a engine + migrations + review fixes (11 tests), 1.9b sqlc pipeline (build-tag generated package, gokit tool, drift gate in CI)
- [x] 1.10 API bootstrap: `cmd/api` composition root with graceful shutdown serving `/health` + `/ready` — landed 2026-10-10; smoke + live-boot verified; 1 MiB body cap in the platform chain
- [x] 1.11 Harness skeleton (`gokit` v0): typed command layer + `verify --profile fast|full|ci` + `doctor` + tool pinning; `make verify` delegates to the full profile — landed 2026-10-10; full gate ≈4.1s, fast ≈1.5s; exit codes 0/1/2
- [x] 1.12 Drift & docs checks: `env.example` ↔ config schema, project map (AGENTS.md tree ↔ dirs), markdown link integrity, ADR index completeness — landed 2026-10-10; all four run first in the fast profile

Exit gate: `make verify` green; `cmd/api` boots; `X-Request-Id` echoed on `/health`, `/ready`; envelope, problem, and validation paths covered by platform tests; generated-artifact drift gate works.

## Phase 2 — Contract pinning

Objective: the pinned contract and the machinery that proves client parity.

- [x] 2.1 Pin backend snapshot: `contracts/backend.openapi.yaml` + `provenance.json` (source commit + SHA-256) — landed 2026-10-10; pinned at backend `670d9bc62e6a` (sha256 `6ae079ad…`); `gokit contract sync|check`; digest marker covered by gate honesty
- [x] 2.2 Generate request/response types (`oapi-codegen`, types only) + `make generate` + drift gate — landed 2026-10-10; oapi-codegen v2.8.0, 1262 generated lines, drift covers both generators
- [x] 2.3 Contract suite harness: live-response validation (`kin-openapi`) + header/status/error-code assertions — landed 2026-10-10; live baseline for `health.get`/`ready.get`; testing-strategy standard in place
- [x] 2.4 Operation allowlist + coverage check inside `make verify`; `vacuum` spec lint — landed 2026-10-10; 34 declared, allowlist-enforced coverage with method/path parity; vacuum v0.33.0 error-severity lint in the full profile
- [x] 2.5 Gate-honesty meta-test: inject a stale snapshot and a forbidden import; both gates must fail with their expected markers — landed 2026-10-10; three scenarios run inside `make verify` (digest, coverage, depguard)

Exit gate: harness runs against the booted API; implemented-vs-allowlisted operation inventory is enforced.

## Phase 3 — Auth slice (clean tier)

Objective: the full auth surface, contract-green.

- [x] 3.1 Schema + migrations: users, credentials, sessions, refresh tokens (sqlite set first) — landed 2026-10-11; sqlite + pg sets, CHECK/FK/cascade/index tests, Down round trip; pg set added early to keep sqlc dual-engine green
- [x] 3.2 Password register + login (argon2id, dummy-hash equalization, rate limits) — landed 2026-10-11; wire-validated register/login, EdDSA tokens, Redis login limiting with Retry-After; allowlist 32 → 30
- [x] 3.3 Sessions: refresh rotation, reuse detection, logout — landed 2026-10-11; rotation with session-family containment, idempotent 204 logout; allowlist 30 → 28
- [x] 3.4 Access tokens: EdDSA keyring, JWKS endpoint — landed 2026-10-11; JWKS live and snapshot-validated, verifier with the reference rejection matrix, principal guard unit-tested; allowlist 28 → 27
- [x] 3.5 Change password — landed 2026-10-11; first protected route (guard + idempotency composed guard-outer), CAS credential update, other-session revocation; allowlist 27 → 26
- [x] 3.6 Email verification + resend — landed 2026-10-15; public verify consumes the token and verifies the address in one transaction, the guarded resend applies the provider gate, the silent no-oracle skip, and the per-account limiter before enqueuing on the emails lane; allowlist 12 → 10
- [ ] 3.7 Password reset request/confirm *(needs 5.2)*
- [x] 3.8 OIDC exchange — landed 2026-10-11; Google id_token verification (JWKS-cached adapter), identity resolve/create with link-required collisions, unified MeView; allowlist 26 → 25
- [x] 3.9 `forbidigo` rules land with the first feature (no `time.Now()` in services, no raw JSON writes) — landed 2026-10-11; rules proven live via gate honesty (driver/raw-JSON/time.Now/platform→features fixtures); six honesty scenarios run in `make verify`
- [x] 3.10 `auth.oidc.connect` — landed 2026-10-12; guarded + idempotent identity link with the reference kind semantics (already-linked is 204, identity/provider conflicts are distinct 409s), conditional email verification when the identity email matches an unverified account; allowlist 25 → 24

Exit gate: contract suite green for all auth operations; rotation/reuse parity assertions pass.

## Phase 4 — Me slice

- [x] 4.1 `/v1/me` GET/PATCH — landed 2026-10-12; new `internal/features/me` slice (guarded, patch idempotent), tri-state patch decode (`httpx.OptionalString`), reference set/clear patch semantics and profile-row upsert; `auth.Register` now returns the shared guard; allowlist 24 → 22
- [x] 4.2 Sessions list/revoke — landed 2026-10-12; first paged endpoint: opaque versioned cursor bound to its sort, keyset pagination over `createdAt`/`id` with an `id DESC` tie-breaker, status/`current` derivation, idempotent revoke that clears device state and refresh tokens; cursor codec closes the 1.7 follow-up; allowlist 22 → 20
- [x] 4.3 Push token upsert/revoke — landed 2026-10-12; `PUT`/`DELETE /v1/me/push-token` with the reference gate (501 `AUTH_PUSH_NOT_CONFIGURED` until `PUSH_ENABLED`), single-transaction ownership hand-off between sessions, revoke without a session-state gate for best-effort sign-out; allowlist 20 → 18
- [x] 4.4 Profile image flow (presign → complete → url → clear) — landed 2026-10-12; stdlib SigV4 object-storage seam (`platform/storage` + fake S3 test server, AWS vectors), `stored_files` schema, per-route idempotency TTL, per-user/per-IP upload limiter, reference 409 mismatch codes with best-effort cleanup; allowlist 18 → 14
- [x] 4.5 Account deletion request/cancel — landed 2026-10-13; 30-day grace schedule with the last-admin 409 invariant, REQUESTED/CANCELED audit rows, no-op repeats, and `accountDeletion` wired through the shared Me read; final de-identification stays 5.4; allowlist 14 → 12

Exit gate: contract suite green for users/me operations.

## Phase 5 — Worker + jobs

- [x] 5.1 Worker bootstrap: asynq server, retries/backoff, `/health` + `/ready` — landed 2026-10-13; `cmd/worker` + `internal/platform/queue` (name validation, `__meta` envelope, `REDIS_URL` options, producer/server, jittered 5-attempt retry policy), probes on `WORKER_HTTP_PORT`, drain on cancel; OTel propagation still pending the otel package
- [x] 5.2 Email jobs + Resend adapter — landed 2026-10-13; `platform/email` (stdlib Resend client, shared templates, `emails` queue), worker-side token minting for `auth.sendVerificationEmail` / `auth.sendPasswordResetEmail` with the reference skip semantics, migration 0006 token tables, `queue.Bind`; unblocks 3.6/3.7
- [x] 5.3 Push jobs + FCM adapter — landed 2026-10-13; stdlib+jwx FCM v1 client with cached service-account auth and the reference failure classes, session-state-machine `push.send` handler with the invalid-token compare-and-set clear, `PUSH_ENABLED` retired for credential-derived enablement; ADC unported
- [x] 5.4 Account deletion jobs (grace period) — landed 2026-10-15; deterministic finalize/reminder/expiry jobs via `queue.Scheduler`, one-transaction de-identification with the FINALIZED audit, last-admin re-check, the two deletion emails, and the profile-image cleanup jobs (4.4 follow-up); schema 0007 on both engines

Exit gate: email verification/reset flows complete end-to-end.

## Phase 6 — gokit CLI + guides

- [x] 6.1 `scaffold feature` / `scaffold endpoint` — landed 2026-10-15; marker-based wiring, snapshot-resolved operations, allowlist hand-off, rollback on failure; template output not yet proven by `make verify` (that is 6.5's smoke)
- [x] 6.2 `remove feature` — landed 2026-10-15; unwires the composition root, returns the feature's operations to the allowlist, and deletes the per-feature files, with text edits before any deletion so refusals leave the tree untouched
- [x] 6.3 `doctor` + completion of `db generate` — landed 2026-10-15; doctor's checks are documented in `docs/guide/config.md`, and `db generate` now cleans its `.gen/` scratch on both success and failure
- [x] 6.4 Guides — landed 2026-10-15; `docs/guide/{adding-a-feature,adding-an-endpoint,config,jobs}.md` written from the implemented behavior and indexed in `docs/README.md`
- [x] 6.5 Scaffold smoke — landed 2026-10-15; `make scaffold-smoke` (and a CI step) copies the tree, scaffolds a feature plus one allowlisted operation, regenerates, and runs the full profile there: `passed — scaffolded output is gate-clean`, which closes 6.1's recorded deviation and surfaced five template defects

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
