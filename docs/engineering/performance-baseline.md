# Performance baseline

One measurement, taken so later numbers have something to be compared against. The
harness lives in the repository (`tools/bench`) so the run can be repeated rather
than trusted, and the commands below are exactly what produced these figures.

## Environment

| | |
| --- | --- |
| Host | AMD Ryzen 7 5800H, 16 cores, 30.7 GiB RAM, Linux |
| Toolchain | go1.27.2 linux/amd64 |
| Lane | SQLite (the default build) |
| Redis | `redis:7` in Docker, localhost |
| Load | 200 sequential requests per route, HTTP over loopback |

## Binary size

`go build ./cmd/...`, no linker flags — what the repository ships:

| Binary | Size |
| --- | --- |
| `api` | 39.0 MiB (40,908,202 bytes) |
| `worker` | 38.4 MiB (40,214,010 bytes) |
| `api`, stripped (`-ldflags="-s -w"`) | 26.9 MiB (28,225,799 bytes) |

The kit does not strip by default: debug symbols are worth more than 12 MiB until a
deployment says otherwise. The stripped figure is recorded so that a container build
can make that trade knowingly.

## Idle memory

Resident set after boot plus three seconds of settling, one process each:

| Process | Idle RSS |
| --- | --- |
| `api` | 44.1 MiB (45,116 KB) |
| `worker` | 23.8 MiB (24,364 KB) |

The worker is smaller because it runs no HTTP router and holds no auth keyring; it
polls its queues and serves its probes.

## Latency

Round trip from the harness process on the same host, so this is server time plus
loopback:

| Route | p50 | p95 | p99 | max |
| --- | --- | --- | --- | --- |
| `GET /health` | 142 µs | 173 µs | 270 µs | 319 µs |
| `GET /v1/me` | 379 µs | 439 µs | 483 µs | 818 µs |
| `GET /v1/me/sessions` | 405 µs | 500 µs | 608 µs | 1.16 ms |

Read them as shape rather than precision. What they say: an unauthenticated probe is
a router hit, an authenticated read costs roughly 300 µs more (guard, session load,
`TouchUser`), and a list adds a keyset query on top of that. Sequential requests
understate throughput, and nothing here exercises concurrency.

## Reproducing

```bash
# a Redis for the API and worker
docker run -d --name gokit-redis -p 56379:6379 redis:7

# signing keys for a local boot (any Ed25519 JWK set works)
go run ./tools/bench/genkeys                    # prints AUTH_SIGNING_KEYS_JSON

# boot, measure, stop
go build -o /tmp/api ./cmd/api
APP_ENV=dev DATABASE_PATH=/tmp/bench.db REDIS_URL=redis://127.0.0.1:56379/0 \
  AUTH_SIGNING_KEYS_JSON=... AUTH_ISSUER=https://bench.local AUTH_AUDIENCE=bench \
  /tmp/api &
go run ./tools/bench -base http://127.0.0.1:8080 -requests 200
```

The harness registers its own account and signs in, so no fixture is needed. For
memory, read RSS after boot: `ps -o rss= -p <pid>`.

## Not measured

- Concurrency: no load generator, so no throughput or saturation numbers.
- The Postgres lane: the same harness works (`DATABASE_URL` + `go build -tags pg`),
  but the numbers above are the SQLite lane's.
- Migration cost at boot, cold-start time, and anything about a real network.
- Cost: this is a single-box template, so the cost model is the box plus object
  storage; the numbers that matter there are the binary and RSS figures above.
