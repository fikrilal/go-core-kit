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
