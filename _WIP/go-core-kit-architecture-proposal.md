# Go Core Kit Architecture Proposal

- Status: Accepted for planning (2026-10-10)
- Date: 2026-10-10
- Scope: go-core-kit repository architecture, contract parity with backend-core-kit, stack baseline, performance/cost model, and developer experience model
- Non-scope: implementation, ADRs, execution plans, CI wiring, endpoint-by-endpoint specification

## Summary

go-core-kit is the Go member of the core-kit family: a production-grade modular monolith (API process + worker process) that serves the **same API contract as backend-core-kit**, so existing frontend-core-kit and mobile-core-kit clients can point at it without tuning.

The proposed direction:

1. **Wire-contract parity is a hard, mechanically verified goal.** The contract is pinned from backend-core-kit's committed OpenAPI snapshot, request/response types are generated from it, and a contract suite validates live responses against it.
2. **Same family architecture.** `platform` owns every cross-cutting concern; features are progressive vertical slices (endpoint → capability → clean); use cases are optional; only composition roots wire things together.
3. **Stack optimized for performance, smallest cost, and readability.** Stdlib HTTP, SQLite + `sqlc` (opt-in Postgres build), Redis + `asynq`, `log/slog`, explicit constructor wiring, no DI container, no ORM, no reflection magic.
4. **DX as a first-class goal.** One verify gate, a `gokit` scaffold CLI that makes the simple path the default path, docs taxonomy mirroring backend-core-kit, and handlers that read top-to-bottom.

## Current context

The repository is empty (git initialized, remote `git@github.com:fikrilal/go-core-kit.git`, no commits). The family convention lives in the sibling kits:

- `backend-core-kit` — contract origin: envelope, RFC7807 problem details, error codes, idempotency, cursor lists, first-party session tokens, worker process, standards docs.
- `mobile-core-kit` — normative source for the house style: simplicity first, optional use cases, progressive feature decomposition, core module that makes feature code tiny.
- `frontend-core-kit` — demonstrates pinned-contract consumption: committed OpenAPI snapshot + SHA-256 provenance + byte-verified drift gate + generated types.

Prerequisite: the Go toolchain is not installed on this development machine. Toolchain setup is phase 0.

## What "clients need no extra tuning" means

Clients depend on three layers. Only the first must be identical; the other two are ours to choose.

| Layer | Definition | Parity requirement |
| --- | --- | --- |
| Wire contract | Paths, methods, JSON schemas, status codes, headers, error codes, token semantics | **Identical for every implemented operation** — machine-verified |
| Behavior details | Rate-limit thresholds, `title` copy, timing, email content | Matched where clients observe them; pinned by parity tests where they do |
| Data layer | Schema, existing rows, hash formats | Independent by default; drop-in reuse is a separate decision (see Accepted defaults) |

### The contract surface that must match

- Success envelope `{ "data": …, "meta"? }`; list meta `{ nextCursor?, limit, hasMore }`; `204` for no-content; `201` + `Location` for creation.
- RFC7807 problem details: `application/problem+json`, `type`, `title`, `status`, `detail?`, `code`, `traceId`, `otelTraceId?`, `errors[]?` (`{field, message}`).
- Headers: `X-Request-Id` accepted and echoed on every response; `Idempotency-Key` on retryable writes with `Idempotency-Replayed: true` on replay; `Retry-After` on `429`.
- Error codes: the same global set plus feature codes (`AUTH_INVALID_CREDENTIALS`, `USERS_EMAIL_ALREADY_EXISTS`, …). Clients branch on these — they are contract, not copy.
- List behavior: cursor-based pagination (`limit`, `cursor`), `sort` with `-` prefix, `filter[...]` allowlists per endpoint.
- Token semantics: access token is an asymmetric JWT with `kid` (EdDSA default, RS256 supported), JWKS at `/.well-known/jwks.json`; refresh token is opaque, stored hashed, rotated on every refresh, reuse-detection revokes the session; access tokens carry minimal claims (`iss`, `aud`, `sub`, `exp`, `iat`, `jti`, `typ`, roles).

### How "100% same" is proven, not assumed

1. **Pin the contract.** Vendor backend-core-kit's committed `docs/openapi/openapi.yaml` into `contracts/backend.openapi.yaml` with `contracts/provenance.json` (backend commit SHA + file SHA-256), mirroring frontend-core-kit.
2. **Generate Go request/response types from the snapshot** so handlers literally speak contract types. Generated code is committed and drift-checked (`make generate` + diff gate).
3. **Contract suite over live responses.** E2E tests run the real API and validate every response body against the pinned snapshot schemas (kin-openapi), including problem details and `x-error-codes` membership.
4. **Parity assertions for non-schema behavior.** The suite also asserts headers (`X-Request-Id` echo, `Idempotency-Replayed`, `Retry-After`, `Location`), status codes, and refresh rotation/reuse semantics — the behaviors clients already rely on.
5. **Operation allowlist.** Operations not yet implemented are listed explicitly; CI fails if an implemented operation is missing from the allowlist or unvalidated.

Result: for the implemented surface, a client cannot tell the difference — and the repo fails CI the moment it could.

## Goals

- Serve the backend-core-kit wire contract exactly for the client-facing surface.
- Make adding an endpoint feel small: a handler that reads top-to-bottom, at most one optional service, one repository.
- Keep production invariants: envelope, problem details, request IDs, idempotency where relevant, stable error codes, clock injection, secrets from env.
- Optimize for performance and smallest cost: static binary, ms startup, low memory, no reflection-heavy frameworks, no database server, one Redis.
- Make the happy path the readable path: plain Go, explicit wiring, SQL visible, generated code marked and committed.
- Enforce boundaries mechanically (lint), not by review memory.

## Non-goals

- Not a drop-in data-layer replacement for backend-core-kit (see Open questions).
- Not a general-purpose Go web framework; no plugin system, no DI container, no ORM.
- Not porting demo fixtures (`merchant-onboarding` is an explicit case study in backend-core-kit).
- Not changing any existing contract; the Go kit follows, never leads.
- Not a multi-package monorepo; one module, two binaries.

## Proposed architecture

### Repository layout

Mirrors the family's top-level discipline (`apps`/`libs` → Go conventions):

```text
go-core-kit/
├─ cmd/
│  ├─ api/                  # HTTP process bootstrap (composition root)
│  └─ worker/               # background jobs bootstrap (composition root)
├─ internal/
│  ├─ platform/             # cross-cutting concerns; never imports features
│  │  ├─ config/            # env load → validate → typed config + invariants
│  │  ├─ httpx/             # envelope, problem details, request id, idempotency,
│  │  │                     #   list query, validation errors, error mapping
│  │  ├─ auth/              # JWT issue/verify, JWKS, keyring, principal context
│  │  ├─ rbac/              # permission checks (phase 2, with admin)
│  │  ├─ db/                # connection policy, write transactions, engine retry
│  │  ├─ redis/             # client + key conventions
│  │  ├─ queue/             # asynq wiring, job names, OTel propagation
│  │  ├─ storage/           # S3-compatible presign/head/delete
│  │  ├─ email/             # Resend adapter behind a port
│  │  ├─ push/              # FCM adapter behind a port
│  │  ├─ logging/           # slog JSON setup + redaction
│  │  ├─ otel/              # OTel init (no-op when env absent)
│  │  └─ health/            # /health, /ready
│  ├─ features/             # product capabilities (vertical slices)
│  │  ├─ auth/
│  │  └─ me/
│  └─ shared/               # framework-free cross-feature contracts
│                           #   (error code unions, cursor types, string/time helpers)
├─ db/
│  ├─ migrations/           # goose migrations (embedded)
│  └─ queries/              # sqlc input (SQL)
├─ internal/db/gen/         # sqlc output (committed, marked generated)
├─ contracts/               # pinned backend snapshot + provenance + generated types
├─ tools/gokit/             # scaffold / remove / verify / doctor CLI
├─ docs/                    # same taxonomy as backend-core-kit
├─ _WIP/                    # proposals and reports (not durable source of truth)
├─ AGENTS.md
├─ Makefile
└─ go.mod
```

### Process model

Two binaries from one module, matching the family baseline:

```text
clients (frontend-core-kit · mobile-core-kit)
   │   same contract: envelope · RFC7807 · X-Request-Id · idempotency · cursor lists · JWT + JWKS
   ▼
┌─ cmd/api ────────────────────────────────┐      ┌─ cmd/worker ──────────────┐
│ platform middleware chain                │      │ asynq consumers           │
│   requestid → logging → recover →        │      │   email · push · deletion │
│   auth → idempotency → list query        │      └───────────┬───────────────┘
│ feature routes (progressive slices)      │                  │
└──────┬────────────────────┬──────────────┘                  │
       │                    │                                 │
       ▼                    ▼                                 ▼
   SQLite                Redis  ◄──────────────────────────────┘
 (sqlc · opt-in PG)   (idempotency · rate limits · queue)
```

- API process serves HTTP and enqueues jobs.
- Worker process consumes jobs, owns retries/backoff, exposes `/health` + `/ready`.
- Both binaries build from the same module with `CGO_ENABLED=0`; identical image, two entrypoints.
- With the default SQLite engine, `cmd/api` and `cmd/worker` must run on the same machine and local filesystem; the opt-in Postgres build lifts this to multi-instance (see `./go-core-kit-dual-engine-data-layer-proposal.md`).

### Platform inventory (what a feature gets for free)

Every item below exists once and is never re-implemented per feature:

| Concern | Provided by | Feature sees |
| --- | --- | --- |
| Request correlation | `platform/httpx` | `X-Request-Id` echo included automatically; `traceId` in every problem |
| Envelope | `platform/httpx` | `httpx.Data`, `httpx.List`, `httpx.Created`, `httpx.NoContent` helpers |
| Problem details | `platform/httpx` | Typed error → RFC7807 mapping; features never write problem JSON |
| Validation errors | `platform/httpx` | Decode/validate helper → `errors[] {field,message}`, `VALIDATION_FAILED` |
| Idempotency | `platform/httpx` + `platform/redis` | Opt-in route flag; Redis lock + cached response + replay header |
| List query | `platform/httpx` | `parseListQuery` with per-endpoint sort/filter allowlists → typed params |
| Auth | `platform/auth` | Bearer verification, principal in `context.Context`, JWKS endpoint, keyring |
| Config | `platform/config` | Typed config structs; fail-fast startup validation and invariants |
| Logging | `platform/logging` | `slog` JSON with request/trace fields and redaction |
| Tracing | `platform/otel` | OTel spans; disabled cleanly when env absent |
| DB | `platform/db` + `internal/db/gen` | Connection policy, write-transaction helpers, engine-specific retry; generated query functions |
| Queue | `platform/queue` | Enqueue helpers, job naming, OTel propagation, worker wiring |
| Storage / email / push | `platform/*` | Ports + adapters; disabled when unconfigured |
| Health | `platform/health` | `/health`, `/ready` |

### Feature tiers (progressive)

Same tiers and promotion triggers as backend ADR 0018 — adopted as-is. Use the smallest tier that fits the behavior.

| Tier | Use when | Shape |
| --- | --- | --- |
| Endpoint slice | Simple CRUD, settings, read/list endpoints, one repository, no complex invariant | `handler.go`, optional `service.go`, `repository.go` (SQL via sqlc), optional `errors.go`, colocated tests |
| Capability slice | Multiple related endpoints share behavior, policies, or persistence | capability folders under the feature with a shared repository/errors layer |
| Clean slice | Auth, money, deletion, queue workflows, transaction-heavy invariants, multiple adapters | explicit `domain` / `app` / `ports` / `infra` packages |

Promotion triggers (any one):

- business rules must be pure and independently unit-tested;
- one use case needs multiple adapters;
- external side effects must be orchestrated through ports;
- transaction boundaries span multiple repositories or aggregates;
- queue retries, idempotency, or eventual consistency affect correctness;
- auth/session/RBAC/security-sensitive behavior is changing;
- the feature is expected to be extracted outside this module;
- repository behavior is complex enough that framework-free service tests materially improve confidence.

Default endpoint-slice shape:

```text
internal/features/preferences/
  preferences.go            # public surface: Deps struct + Register(mux, deps)
  handler.go                # decode → call → respond; thin
  service.go                # optional — only when logic exists
  repository.go             # persistence boundary; sqlc query functions live here
  preferences_errors.go     # only when feature-specific branchable codes exist
  handler_test.go
  service_test.go
```

### Optional use cases (rule)

In Go, a use case is a plain service struct. The mobile-core-kit rule applies unchanged:

- Add a service only when the operation has logic worth isolating: final validation/normalization gate, multi-step orchestration, or a business rule worth its own table-driven test.
- Never a pure pass-through. If `Execute` just forwards to one repository call, the handler calls the repository directly.
- A service may be thin; it must not exist for ceremony.

```go
// Exists because it owns the raw-input → validated-aggregate transition.
func (s *Service) Login(ctx context.Context, in LoginInput) (Session, error) { … }

// Does not exist: GET /v1/me/preferences → handler → repository directly.
```

### Errors and results

- **Expected business outcomes are typed results, not errors** (`ChangePasswordResult{Kind: Ok | NotFound | PasswordNotSet | CurrentPasswordMismatch}`), so handlers switch explicitly. Mirrors backend's discriminated unions.
- **Unexpected failures are typed errors** implementing `httpx.ProblemError` (status, stable code, optional issues, optional retry-after). One central mapper writes RFC7807; features never serialize errors themselves.
- Feature codes are typed constants in `internal/shared/<feature>/codes.go`, unioned into the app code set; raw strings are not accepted by the error constructor.
- `errors.As`-style mapping keeps special cases (e.g. `401` for unusable principals) explicit in the feature, matching backend behavior.

### Composition and wiring

No DI container, no reflection. Composition roots only:

```go
// cmd/api/main.go
cfg := config.MustLoad()
pool := db.MustOpen(cfg.Database)
rdb := redisclient.MustOpen(cfg.Redis)
verifier := auth.MustKeyring(cfg.Auth)
mux := httpx.NewRouter(httpx.Deps{Config: cfg, Log: log, DB: pool, Redis: rdb, Auth: verifier})

auth.Register(mux, auth.Deps{…})
me.Register(mux, me.Deps{…})
```

Each feature exposes one `Deps` struct and one `Register` function — its entire public surface. Handlers, services, and repositories are constructed explicitly with constructor injection. Everything is trivially testable with real structs; no framework lifecycles.

### Request flow

```text
HTTP request
  → middleware: request id → logging → recover → (auth | public) → (idempotency if opted in)
  → stdlib mux (method + pattern routing)
  → handler: decode + validate → [service] → repository (sqlc) → SQLite (Postgres in the swap build)
  → httpx.Data / List / Created / NoContent
  → central error mapper (on the way out) renders problem details
side effects: enqueue asynq job → worker consumes → email / push / deletion
```

## Stack decisions (accepted 2026-10-10)

All slots below are decided and become short ADRs. Alternatives are kept for context; changing a decision later requires a superseding ADR.

| Concern | Decision | Why | Alternatives / notes |
| --- | --- | --- | --- |
| Language | Go, current stable (pin exact in `go.mod` + toolchain directive) | Static binary, low RSS, ms startup | — |
| HTTP | Stdlib `net/http` (method+wildcard patterns) + ~200 lines of middleware | Zero framework cost; explicit; fastest | `chi` only if route groups outgrow stdlib |
| DB access | SQLite first choice (`modernc.org/sqlite` + `sqlc`); Postgres as an opt-in build | In-process database: nothing to run, tune, or back up; SQL-first, compile-time checked, plain generated Go | Dual-engine swap design: `./go-core-kit-dual-engine-data-layer-proposal.md` |
| Migrations | `goose` with embedded per-engine migration sets | Simple, committed, runs from the binary | `golang-migrate`, `tern` |
| Redis | `go-redis/v9` | Idempotency + rate limits (Redis already required) | — |
| Queue | `asynq` | Redis-backed → one broker; works identically in both engine builds | `river` (Postgres-only; incompatible with the SQLite default), reimplementing BullMQ wire format (only if ever sharing queues with Node — not a goal) |
| JWT / JWKS | `lestrrat-go/jwx/v3` | Full multi-`kid` keyring + JWKS generation; EdDSA and RS256 | `golang-jwt/v5` + hand-rolled JWKS |
| Password hashing | `x/crypto/argon2` (argon2id, PHC string format) | Matches standard; PHC encodes params | — |
| Validation | `go-playground/validator/v10` behind a small `httpx` wrapper | Maps cleanly to `errors[] {field,message}` | Hand-rolled per-DTO checks (verbose) |
| Config | `caarlos0/env` + explicit `Validate()` invariants | Typed structs, fail-fast, readable | Hand-rolled stdlib parsing (zero-dep) |
| Logging | `log/slog` JSON handler + redaction | Stdlib, zero-dep, structured | — |
| Tracing | OTel SDK + OTLP HTTP exporter | Disabled cleanly when env absent | — |
| Storage | `aws-sdk-go-v2` S3 client (R2/S3-compatible endpoint) | Presign put/get, head, delete | — |
| Email | Thin internal HTTP adapter (Resend API) | Dep-light, fenced behind a port | Official SDK if it earns its weight |
| Push | FCM HTTP v1 with `x/oauth2/google` token source | Lighter than admin SDK | `firebase-admin-go` |
| Contract types | `oapi-codegen` (types only) from pinned snapshot | Handlers speak contract types; readable input | Full server-interface generation (rigid; not selected) |
| Contract test | `kin-openapi` response validation + parity assertions | The mechanism that proves "clients need no tuning" | — |
| Spec lint | `vacuum` (Go-native Spectral-compatible) | No Node in the Go kit toolchain | Spectral via CI container |
| Boundaries | `golangci-lint` + `depguard` | Executable import rules | `go-arch-lint`, `import-boss` |
| CLI | `tools/gokit` (scaffold / remove / verify / doctor) | Mirrors `mobilekit` / `backendkit` / `frontendkit` | Makefile-only at first, CLI in its own phase |
| Containers | Multi-stage build → distroless, static binary | ~20 MB image, tiny RAM | — |

## Boundaries (executable rules)

Enforced by `depguard` + `forbidigo` in `golangci-lint`, not by review:

- `internal/platform/*` must not import `internal/features/*`.
- `internal/shared/*` stays dependency-free (stdlib only) and framework-free.
- `internal/features/*` must not import `cmd/*`.
- Cross-feature imports only through a feature's exported `Deps`/`Register` surface, explicitly allowlisted per pair, acyclic. Default scaffold creates none.
- Features must not write raw JSON responses: `encoding/json` writes and direct `w.Write` are forbidden in `internal/features/**` (use `httpx` helpers). They may `json.Unmarshal` request bodies only through the `httpx` decode helper.
- SQL lives in repository files and `db/queries`; handlers and services never build queries.
- `time.Now()` is forbidden in service/domain code; a `now func() time.Time` (or `Clock`) dependency is injected. Wall-clock reads allowed in platform adapters and composition roots.
- Vendor SDKs (`aws-sdk-go-v2`, FCM credentials, Resend) are importable only inside their `platform` package.
- Database drivers (`modernc.org/sqlite`, `pgx`, `database/sql`) are importable only inside `internal/platform/db` and `internal/db/gen`; features never import a driver.

## Performance and cost model

- **Static binary, ms startup** → smallest container sizes on any platform; scale-to-zero friendly; no JIT warmup.
- **No ORM, no reflection** → sqlc-compiled SQL and an in-process SQLite database; predictable allocations.
- **No database server** → SQLite is a file inside the process; Redis is the only external service (queue, idempotency, rate limits).
- **Two processes, one image** → the worker can run beside the API on the smallest instance, or scale separately.
- **Baseline budget to record once (not enforced in CI):** image size, idle RSS for api and worker, p95 latency for `/health` and one representative read endpoint plus one write endpoint, on the smallest target instance. Numbers land in `docs/engineering/`.
- Contract parity does not cost runtime: validation in tests only.

## Developer experience model

- **The happy path is the default path.** `gokit scaffold feature <name>` emits a working endpoint slice (handler, optional service stub omitted by default, repository, tests) that passes `make verify`; `gokit scaffold endpoint` adds routes. `gokit remove feature` unwires cleanly.
- **Handlers read top-to-bottom:** decode/validate → call service or repository → respond. No middleware surprises, no decorators, no magic.
- **Wiring is boring on purpose:** if you want to know what a feature depends on, read its `Deps` struct.
- **SQL is visible:** sqlc keeps the SQL in `.sql` files and the generated Go is plain and marked `Code generated by sqlc. DO NOT EDIT.`
- **One gate:** `make verify` = fmt + vet + golangci-lint (boundaries) + `go test ./...` + contract check + build. CI runs the same gate.
- **Docs mirror backend-core-kit:** `AGENTS.md` (operating contract) + `docs/README.md` index with `core/`, `standards/`, `guide/`, `engineering/`, `exec-plans/`, `adr/`, `openapi/` (pinned contract + README).
- **Errors are where you expect them:** every feature maps its failures in one place; user-facing copy never leaves the HTTP edge.

## Verification and contract gates

1. `make verify` (local gate; CI mirrors it).
2. `make generate` + drift diff (sqlc output, contract types).
3. Contract suite: e2e per engine lane (SQLite and Postgres builds) with real Redis; validates responses against the pinned snapshot; asserts headers, statuses, error codes, and token rotation/reuse behavior.
4. Operation coverage check: implemented operations ⊆ snapshot; unimplemented ones must appear in the explicit allowlist.
5. Boundary lint (depguard) and static analysis.
6. Runtime evidence (when needed): auth/session flows and queue reliability get recorded live evidence, matching backend's practice.

## Phases (high level)

1. **Foundation** — toolchain setup, module scaffold, Makefile, lint config, CI skeleton, `AGENTS.md`, docs taxonomy.
2. **Platform core** — config, logging, httpx (envelope/problem/request-id/validation/idempotency/list-query), db (SQLite engine: sqlc + goose + platform/db), redis, health/ready.
3. **Contract pinning** — vendor snapshot + provenance, generated types, contract suite harness, operation allowlist.
4. **Auth slice** — password register/login/refresh/logout/change, sessions + JWKS; then email verification + password reset (with worker email jobs); then OIDC exchange. Clean-slice shape.
5. **Me slice** — `/v1/me` GET/PATCH, sessions list/revoke, push tokens, profile image flow, account deletion request/cancel.
6. **Worker + jobs** — asynq wiring, email/push/deletion jobs, retry/backoff, OTel propagation.
7. **`gokit` CLI + guides** — scaffold/remove/verify/doctor; `docs/guide/adding-a-feature.md` etc.
8. **Admin + RBAC (tier 2, optional)** — only if a product needs it.
9. **Parity + budget closure** — full e2e parity suite green; performance/cost baseline recorded.

## Risks and tradeoffs

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Hand-building the platform in Go takes effort | Slower start than Node's ecosystem | Port semantics module-by-module from backend; keep implementations thin; phases order platform before features |
| Two codebases drift | Go kit silently diverges from contract | Pinned snapshot + provenance + live-response validation + operation allowlist; CI fails on drift |
| sqlc/codegen adds a build step | Contributors need `make generate` | Generated code committed; drift gate; scaffold runs generation |
| asynq is not BullMQ | Cannot share queues with Node workers | Not a goal; queues are process-internal |
| Full parity scope creep (admin, demo fixtures) | Effort without client value | Explicit tiering; `merchant-onboarding` not ported; admin deferred |
| Go toolchain missing on dev machine | Nothing runs | Phase 0 prerequisite; documented in README prerequisites |
| Behavioral parity gaps not expressible in OpenAPI | Clients "tuned" despite schema parity | Parity assertions for headers/statuses/rotation; pin anything a client observes |
| SQLite single-node ceiling | Default engine cannot scale horizontally | Opt-in Postgres build (dual-engine proposal); single-box deployments documented |

## Acceptance criteria

- Direction reviewed and accepted; stack slots converted into short ADRs.
- `make verify` passes from a clean clone with the documented toolchain.
- Scaffold produces a working endpoint slice that passes `make verify` untouched.
- Contract suite green: every implemented operation validates against the pinned snapshot; headers, statuses, error codes, and refresh rotation/reuse asserted; unimplemented operations explicitly allowlisted.
- `X-Request-Id`, envelope, and problem-details behaviors asserted end-to-end.
- Idempotent write replay returns the cached response with `Idempotency-Replayed: true`.
- Performance/cost baseline recorded (image size, idle RSS, p95 on reference endpoints).
- `AGENTS.md` + docs taxonomy in place; the simple path is documented and is the scaffold default.

## Accepted defaults

All defaults below are accepted (2026-10-10) and become durable decisions via ADRs:

1. **Contract scope for v1** — Client-facing surface only (auth, me/*, jwks, health/ready); admin deferred; `merchant-onboarding` not ported.
2. **Data-layer compatibility** — Independent schema; wire parity is mandatory. Revisit only if a product needs to swap an existing backend deployment onto the Go kit.
3. **Queue tech** — `asynq` (Redis-backed). Reuses the Redis instance already required for idempotency and rate limits.
4. **Config parsing** — `caarlos0/env` with explicit invariant validation.
5. **Spec lint tool** — `vacuum` (Go-native).
6. **CLI name** — `gokit`.
7. **Go version** — latest stable at scaffold time; toolchain directive enforced in CI.
8. **Data layer engine** — SQLite is the first choice (default build); the opt-in Postgres swap is specified in `./go-core-kit-dual-engine-data-layer-proposal.md`.

Durable decisions move into ADRs and normative docs. This proposal remains in `_WIP/` and is not a permanent parallel source of truth.
