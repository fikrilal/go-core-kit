# Project Architecture

go-core-kit is a modular monolith with two processes — an HTTP API and a background worker — that serves the wire contract owned by `backend-core-kit`. Existing clients (frontend-core-kit, mobile-core-kit) observe identical envelopes, problem details, headers, error codes, and token semantics; the Go implementation is not visible to them.

## Principles

- **Contract parity first.** The pinned OpenAPI snapshot is the contract; this kit follows it, never leads. Drift fails CI. See `docs/adr/0002-wire-contract-parity-with-backend-core-kit.md`.
- **Platform owns cross-cutting concerns.** Request IDs, envelope, problem details, auth, idempotency, rate limits, config, logging, tracing, DB, and queue live in `internal/platform` and are never re-implemented per feature.
- **Progressive feature architecture.** Start with the smallest feature shape that fits current behavior and promote only on explicit triggers. See `docs/adr/0001-modular-monolith-and-progressive-feature-tiers.md`.
- **One bootstrap per process.** Composition roots wire config, logging, tracing, and features. No DI container, no reflection — constructor injection only.
- **Transport is not domain.** Features return typed data and typed errors; `httpx` renders envelopes and problem details.

## Repository Layout

```text
go-core-kit/
├─ cmd/
│  ├─ api/                  # HTTP process bootstrap (composition root)
│  └─ worker/               # background jobs bootstrap (composition root)
├─ internal/
│  ├─ platform/             # cross-cutting concerns; never imports features
│  │  ├─ config/            # env load → validate → typed config + invariants
│  │  ├─ httpx/             # envelope, problem details, request id, idempotency, list query
│  │  ├─ auth/              # JWT issue/verify, JWKS, keyring, principal context
│  │  ├─ rbac/              # permission checks (with admin, later)
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
│  └─ shared/               # framework-free cross-feature contracts
├─ db/
│  ├─ migrations/           # goose migrations: sqlite/ and pg/ sets
│  └─ queries/              # sqlc input; portable subset in common/
├─ internal/db/gen/         # sqlc output (build-tag selected engine, committed)
├─ contracts/               # pinned backend snapshot + provenance + generated types
├─ tools/gokit/             # scaffold / remove / verify / doctor CLI
├─ docs/                    # standards, ADRs, exec plans, engineering notes
└─ _WIP/                    # proposals (historical, not source of truth)
```

## Process Model

- `cmd/api` serves HTTP and enqueues background jobs.
- `cmd/worker` consumes jobs (email, push, account deletion), owns retries/backoff, and exposes `/health` + `/ready`.
- With the default SQLite engine, both processes run on the same machine and local filesystem. The `-tags pg` build removes this constraint. See `docs/adr/0004-sqlite-first-with-build-time-postgres-swap.md`.
- Both binaries build with `CGO_ENABLED=0`.

## Feature Tiers

Use the smallest tier that fits the behavior.

| Tier | Use when | Typical shape |
| --- | --- | --- |
| Endpoint slice | Simple CRUD, settings, read/list endpoints, one repository, no complex invariant | handler, optional service, repository, optional errors, colocated tests |
| Capability slice | Multiple related endpoints share behavior, policies, or persistence | capability folders under the feature with a shared repository/errors layer |
| Clean slice | Auth, deletion, queue workflows, transaction-heavy invariants, multiple adapters | explicit `domain` / `app` / `ports` / `infra` packages |

Promotion triggers (any one):

- business rules must be pure and independently unit-tested;
- one use case needs multiple adapters;
- external side effects must be orchestrated through ports;
- transaction boundaries span multiple repositories or aggregates;
- queue retries, idempotency, or eventual consistency affect correctness;
- auth/session/RBAC/security-sensitive behavior is changing;
- the feature is expected to be extracted outside this module;
- repository behavior is complex enough that framework-free service tests materially improve confidence.

### Optional use cases

A use case is a plain service struct. Add one only when the operation has logic worth isolating:

- final validation/normalization gate;
- multi-step orchestration;
- a business rule worth its own table-driven test.

Never a pure pass-through: if the service would only forward to one repository call, the handler calls the repository directly. A service may be thin; it must not exist for ceremony.

```go
// Exists because it owns the raw-input → validated-aggregate transition.
func (s *Service) Login(ctx context.Context, in LoginInput) (Session, error)

// Does not exist: GET /v1/me/preferences → handler → repository directly.
```

## Conventions

### Errors and results

- Expected business outcomes are typed results (`Kind` switch, e.g. `ChangePasswordResult`), so handlers branch explicitly.
- Unexpected failures are typed errors carrying status + stable code (+ issues, retry-after) and are rendered once by the central problem mapper. Features never serialize errors.
- Error codes are typed constants; raw production code strings are rejected by construction.
- Special status mappings (for example, `401` for unusable principals) stay explicit in the feature.

### Composition and wiring

- Each feature exposes one `Deps` struct and one `Register` function — its entire public surface.
- Handlers, services, and repositories are constructed explicitly in composition roots or feature registration.

### Request flow

```text
HTTP request
  → middleware: request id → logging → recover → (auth | public) → (idempotency if opted in)
  → stdlib mux (method + pattern routing)
  → handler: decode + validate → [service] → repository (sqlc) → SQLite (Postgres in the swap build)
  → httpx.Data / List / Created / NoContent
  → central error mapper renders problem details on the way out
side effects: enqueue asynq job → worker consumes → email / push / deletion
```

## Boundaries (enforced)

Enforced by `depguard` and `forbidigo` in golangci-lint, not by review memory. See `docs/adr/0008-boundaries-via-golangci-lint-and-depguard.md`.

- `internal/platform/*` must not import `internal/features/*`;
- `internal/features/*` must not import `cmd/*`;
- cross-feature imports only through a feature's exported `Deps`/`Register` surface, allowlisted per pair, acyclic;
- `internal/shared/*` stays stdlib-only and framework-free;
- database drivers (`modernc.org/sqlite`, `pgx`, `database/sql`) are importable only inside `internal/platform/db` and `internal/db/gen`;
- features must not write raw JSON responses — use `httpx` helpers;
- SQL lives in repository code and `db/queries`; handlers and services never build queries;
- no `time.Now()` in service/domain code — inject the clock;
- vendor SDKs (S3, FCM, Resend) are importable only inside their `platform` package.

## Data Layer

- SQLite is the default engine (`modernc.org/sqlite`); Postgres is an opt-in build (`-tags pg`). Selection is build-time, never runtime. See `docs/adr/0004-sqlite-first-with-build-time-postgres-swap.md`.
- Query access is sqlc; migrations are goose per engine; the portable SQL subset keeps one query set valid for both engines. See `docs/adr/0005-sqlc-goose-and-portable-sql-subset.md`.
- IDs are UUIDv7 generated in Go; timestamps are UTC and flow from the injected clock.

## Contract Parity Model

1. backend-core-kit's committed `openapi.yaml` is pinned into `contracts/` with provenance (source commit + file digest).
2. Request/response types are generated from the snapshot; handlers are hand-written against those types.
3. A contract suite validates live responses against the pinned snapshot and asserts headers, statuses, error codes, and token rotation/reuse semantics.
4. Implemented operations must be a subset of the snapshot; unimplemented operations are listed in an explicit allowlist that CI enforces.

## Pointers

- Stack decisions: `docs/core/project-stack.md`
- Decision log: `docs/adr/README.md`
- Proposals (historical): `_WIP/go-core-kit-architecture-proposal.md`, `_WIP/go-core-kit-dual-engine-data-layer-proposal.md`
