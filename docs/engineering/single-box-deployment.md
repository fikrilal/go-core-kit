# Single-box deployment

The kit's default shape is one host running the API, the worker, Redis, and the
SQLite file, with TLS terminated in front. This note records what that implies and
how to back it up. It describes the deployment the repository supports today; it
is not a claim of high availability.

## Processes

- **`api`** — serves the wire contract and applies migrations at startup
  (`database.Migrate`, SQLite migrations embedded in the binary). It also owns the
  delayed-job scheduler, so the account-deletion and upload-expiry tasks are armed
  by the API process, not the worker.
- **`worker`** — consumes the queues its config lists (`default`, `emails`,
  `push`, `users`) and serves `/health` and `/ready` on `WORKER_HTTP_PORT`
  (8081 by default). It deliberately does **not** migrate: it assumes the schema
  the API applied, so start the API first on a fresh box.

Both drain on `SIGINT`/`SIGTERM`: the HTTP server shuts down, and the worker stops
picking up new tasks before exiting.

## Start order

1. `api` — applies migrations, serves HTTP, arms scheduled jobs.
2. `worker` — begins consuming.

Reversing the order on a fresh database means the worker runs against tables that
do not exist yet; asynq retries will keep failing until the schema lands.

## Backup with Litestream

SQLite is the source of truth, so the backup story is the database file:

- Run SQLite in WAL mode (the kit's connection setup does) and let Litestream
  replicate the database to object storage continuously. A replica is a
  near-real-time copy, not a point-in-time archive: Litestream keeps the
  transaction history it needs to restore, and pruning it early shrinks how far
  back you can go.
- Back up the object storage bucket separately — the profile-image objects and the
  replica both live there, and losing the bucket loses both.

### Restore runbook

1. Stop the worker, then the API.
2. `litestream restore -o <database-path> <replica-url>`.
3. Start the API; it applies any migrations the replica predates.
4. Start the worker and confirm `/ready` on both processes.

## Redis caveats

One Redis instance serves three different jobs: rate limiting and idempotency for
the API, and the queue broker for delayed and background work. Two consequences
worth knowing:

- Losing Redis loses **rate-limit counters and idempotency records** — clients can
  retry into an empty window — and it loses pending queue tasks.
- Scheduled deletion work is recoverable: the API re-arms the finalize job from the
  stored schedule when the account owner next touches the deletion endpoints, so a
  lost task is not a lost account state. Background sends (verification, reset,
  deletion emails) are not replayed, because their tokens live in the database and
  the user can request another.

## What this does not cover

- No HA: one host, one SQLite writer, one Redis. Vertical scaling and a longer
  restart window are the trade.
- No Postgres lane yet — `make verify-pg` and the pg repository are Phase 7.
- No secret management beyond environment injection: secrets never live in the
  repository, and `env.example` documents the keys rather than their values.
- Litestream is not a substitute for testing restores. Restore into a scratch box
  periodically; a replica you have never restored is a hypothesis.

## Operational surface

- `gokit doctor` — checks the repository files, toolchain, and pinned tools. The
  goose CLI is optional and only needed for manual migrations.
- `env.example` — the deployment contract for configuration keys.
- `make verify` and `make scaffold-smoke` — build-time gates, not runtime probes.

## Several projects on one host

The lightest shape when the box is shared is one **compose project per
application**, not one shared service set. Each project gets its own `api`, its own
`worker`, its own Redis, its own SQLite volume, and its own Litestream sidecar —
around 85 MiB idle and two small images (`distroless/static` with the stripped binary
is ~27 MiB). Twelve gigabytes of RAM holds far more of these than a 2-core host can
usefully serve.

Rules that keep the projects independent:

- **One Redis per project**, in the same compose file. A shared Redis is tempting at
  10 MiB, but the limiter and idempotency keys are not namespaced per project: a
  shared instance means one project's keys, evictions, or a `FLUSHDB` reach the
  others. Isolate, or accept that blast radius knowingly.
- **Publish one port per project**, and put a reverse proxy (Caddy is ~30 MiB) in
  front for hostnames and TLS. Give the SQLite volume a named volume, and cap each
  container (`mem_limit`, `cpus`) so one project cannot starve the others.
- **Build on the box or in CI, but pin the platform.** Oracle's Ampere instances are
  `linux/arm64`; an image built on an x86 laptop needs `docker buildx --platform` or
  a CI builder, or it will not start.
- **Keep the SQLite lane per project.** A shared Postgres with one database per
  project is the heavier alternative worth considering once several projects want
  the pg lane; it costs a 646 MB image and ~128 MiB of buffers once, and adds a
  server to administer.

The trade against bare systemd units: units reach ~68 MiB per project with no Docker
daemon, but every project then owns its ports, user, unit files, log handling, and
Redis supervision by hand. Compose costs one daemon (~50–100 MiB, shared) and buys
per-project process and network isolation, declarative restarts, and a single file to
deploy — which is why it is the recommendation for a shared host.

## The container variant

`Dockerfile` builds both binaries in one stage and produces two images from
`distroless/static:nonroot` — measured at **44.5 MB** for the API and **43.9 MB** for
the worker on amd64, which is the stripped binary plus the base image. Build for an
Ampere host with `docker buildx build --platform linux/arm64 --target api .`, or build
on the box itself.

Copy `env.example` to `.env` for the deployment keys. The compose file treats it as
optional, so a clean checkout boots with the development defaults; set `APP_ENV=prod`
and the signing keys in `.env` before exposing the stack.

`compose.yaml` runs the four services this kit needs:

- **redis** — no persistence by intent (`--save "" --appendonly no`): the limiter
  counters, idempotency records, and the queue are caches, and the deployment note
  above records what losing them costs.
- **api** — migrates at boot and serves `HTTP_PORT`; the only published port.
- **worker** — `depends_on: [api]`, because it assumes the schema the API applied.
- **litestream** — behind the `backup` profile, because it needs a replica
  configured (`docker compose --profile backup up -d`). Its `LITESTREAM_*` keys are
  deployment settings for the sidecar, not application configuration, so they are
  documented in `litestream.yml` rather than `env.example`.

The SQLite file lives on a bind mount (`./data`) because a named volume is created
root-owned and the images run as uid 65532. An **init service** creates the directory
and hands it over once, so a fresh checkout boots with no manual step; the mount also
makes the file the operator's to back up and inspect, which is the point.

Verified by booting the stack: `/health` and `/ready` answer 200 through the published
port, the database file appears on the mount, and idle memory measured 26 MiB (api),
6 MiB (worker), and 4 MiB (redis) — comfortably under the compose limits, which exist
to protect neighbour projects on a shared host.
