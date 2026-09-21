# Section note: `cmd/api`

- Status: Draft, waiting for acceptance
- Date: 2026-10-11
- Scope: the composition root. `main.go`, `server.go`, `server_test.go`, `helpers_test.go`.
- Out of scope: the contract suites (`contract_*.go`, `coverage_test.go`). They call `newServer` and then assert wire shapes. They do not block reading boot, so they are not a second note. The allowlist reader is section 8.
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: none in this pass
- Earlier notes stay unapplied

## Verdict

Boot is already a composition root. `main` loads config and installs the signal. `run` opens SQLite, migrates, opens Redis, builds the server, and listens. `newServer` is the only place that turns config integers into durations and calls `auth.Register`. `serve` drains on cancel. No business rule lives here.

The hard lines are the ones a new Go reader cannot see from the names: why `main` calls `os.Exit`, why shutdown uses `context.WithoutCancel`, why the shutdown budget is 10 seconds and the header timeout is 5, and why `SessionTTL` is filled from `AuthRefreshTokenTTLSeconds`.

One wire is dead. `auth.Deps.Logger` is set here and never read. The feature does not log.

No live bug.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `main.go` | 81 | Config, signal, open, migrate, listen |
| `server.go` | 83 | Router, auth wiring, serve, shutdown |
| `server_test.go` | 137 | Real HTTP probes and cancel |
| `helpers_test.go` | 52 | `testConfig` and a generated signing key |

Plan 1.10 is the decision log: `runMain` returns an int so `os.Exit` does not skip defers, `ListenConfig` so the signal can cancel the bind, `WithoutCancel` because the parent is already canceled, `Open` rather than `MustOpen`, and the `!pg` tag until a Postgres engine exists. The smoke test's keep-alive note is why `testClient` disables pooling: a half-read request holds `Shutdown` until `ReadHeaderTimeout`, and that timeout must stay under the 10 second drain.

`coverage_test.go` `coverageRouter` builds a server against `redis://127.0.0.1:1/0`. That call succeeds. Boot does not dial Redis. Same fact as the redis note, visible here as the process order.

## Hard to read

### 1. `os.Exit` is why `main` is one line

```go
func main() {
    os.Exit(runMain())
}
```

`os.Exit` does not run defers in `main`. The defers that close the database and stop the signal live in `run` and `runMain`. `runMain` returns the status code so those defers run, and only then does the process exit. `tools/gokit` uses the same split. Say that on `main`. Without the sentence, the extra function looks like ceremony.

### 2. Shutdown detaches from the signal context

On `ctx.Done()`, `serve` does:

```go
shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
```

The signal has already canceled `ctx`. A timeout derived from `ctx` would be expired immediately, and `Shutdown` would not drain. `WithoutCancel` keeps the values and drops the cancel. The 10 second limit is the new context. Plan 1.10 records this. The code does not.

`ReadHeaderTimeout` is 5 seconds, inline. A request stuck while reading headers holds `Shutdown` until that timeout. The drain is 10 seconds so one stuck header read still finishes inside the budget. The two numbers are a pair. Name the 5 seconds next to `shutdownTimeout` and say why the drain is longer. Do not change either number.

### 3. `SessionTTL` is the refresh-token field

```go
SessionTTL: time.Duration(cfg.AuthRefreshTokenTTLSeconds) * time.Second,
```

The environment variable is the refresh lifetime. The feature stores that same instant on the session and on the refresh token. The access-token TTL is the other field and stays on the platform issuer. The translation belongs in this function. Do not add a second config type for one caller. One comment on this line is the missing name.

The other integer-to-duration lines are what they say (`LoginWindow`, `LoginBlock`, `AccessTokenTTL`).

### 4. `auth.Deps.Logger` is never read

`newServer` passes `logger` into `auth.Deps`. The field is the only `slog` use in `internal/features/auth`. No function reads it. Access logs come from `httpx.NewRouter(logger)`. Readiness logs come from `health.New(logger)`. A reader of this wiring thinks the feature logs with that logger.

Delete the field and the assignment. That is the whole change. `Register` does not gain a log call in this pass.

## Correct Go that should stay

- `run` order is the boot. Config failure prints to stderr because the logger does not exist yet. Everything after that is a wrapped error and a structured log. SQLite is migrated before listen. Redis `Open` only parses the URL. A down Redis still lets the process serve, and `/ready` returns 503. That is plan 1.6. Do not ping in `run`.
- `net.ListenConfig.Listen` takes the signal context, so a signal during bind returns instead of leaving a socket behind. `http.Server.Addr` is only that bind string. `Serve` uses the listener `run` opened. Tests ignore `Addr` and bind `127.0.0.1:0` or use `httptest`.
- `serve` treats `http.ErrServerClosed` as a clean stop. Any other `Serve` error is wrapped. After a successful `Shutdown` it does not read the `Serve` goroutine's result. The channel is buffered, the handlers have finished, and the defers in `run` then close Redis and the database. Close order is last-opened first. Leave it.
- `clock.System{}` is the production clock. Tests that need a frozen clock build `app.Service` themselves. Contract tests use the real clock and a 900 second access token, which is long enough for the assertion.
- `WithIdempotency` is on the router before `auth.Register`. The change-password route panics at boot if that option is missing. The call order is the requirement. Idempotency still wraps only the routes that ask for it.
- `health.New(...).Register(router)` does not keep the health value. The routes are the result.
- The `!pg` tag is on every file in this directory. A Postgres build skips the binary instead of failing compile. One package comment on `main.go`.
- `testConfig` is not `config.Load`. It sets `EnvTest`, the integer defaults, and a generated Ed25519 key. Issuer and audience stay empty, which is legal because the test env is not production-like. `testClient` disables keep-alives so `Shutdown` is not stuck on a pooled half-read. Both stay.
- `server_test.go` cancels the context. It does not send `SIGTERM` to the test process. `runMain`'s signal setup stays an unread line. Do not add a test that signals the test binary.
- The contract files each open a database, migrate, start miniredis, and call `newServer`. That is one fixture per route family. Do not merge them in this pass.
- `http.Server.ErrorLog` is still the standard library logger. A bad connection can write a non-JSON line. It is quiet in the boot plan's live run. Do not add a slog adapter here.

## Real bugs

None.

Checked and not bugs: boot does not dial Redis, `Shutdown` detaches from the canceled signal, the header timeout is shorter than the drain, empty issuer in `testConfig`, and the unused auth logger (dead wire, not a wrong response).

## Design question

What is still in `cmd/api`, and what must stay here?

This directory owns process life and wiring only. The duration conversion stays in `newServer` because it is the composition root and it has one caller. Feature policy stays in `internal/features/auth`. Platform mechanics stay in platform.

Do not move `serve` into `httpx`. The next process (`cmd/worker`, when it exists) can copy `runMain` and `serve`. It should not import the API server.

## Function notes

### `main.go`

Finding 1. `runMain` loads config, builds the logger, and arms `SIGINT` and `SIGTERM`. `run` is finding 3's boot order plus the listen. `closeAndLog` is the defer body. A close error is logged and does not replace the return value of `run`. Leave that: a failed close after a clean shutdown should not turn exit 0 into exit 1. The plan's live stop is exit 0.

### `server.go`

Findings 2, 3, and 4. `newServer` is the wiring list. `serve` is the select. The select can take either ready channel when a serve error and a signal happen together. That race is not a live failure mode. Leave the select.

### Tests

`TestServeServesProbesAndShutsDown` is the boot proof: `/health` 200, `/ready` 200, `/ready` 503 when miniredis errors, then `serve` returns nil after cancel. `waitForStatus` polls with `time.Now`. That is a test deadline, not a domain clock. `forbidigo` allows `time.Now` in `cmd`.

## What acceptance authorizes

Taste:

1. Delete `auth.Deps.Logger` and the assignment in `newServer`. Drop the unused `log/slog` import in the feature.
2. Name the 5 second header timeout next to `shutdownTimeout`. No value changes.
3. Comments, and nothing else:
   - `main`: `os.Exit` skips defers, so `runMain` returns the status.
   - `main` package: SQLite build until a pg process exists.
   - `run`: migrate before listen; Redis `Open` does not dial; `/ready` is the check.
   - `ListenConfig`: a canceled context aborts the bind.
   - `SessionTTL`: this field is `AUTH_REFRESH_TOKEN_TTL_SECONDS`, and the feature uses it for the session and the refresh token.
   - `serve`: `WithoutCancel` because `ctx` is already canceled; the header timeout holds `Shutdown`, so the drain is longer.
   - `testConfig`: not `Load`; empty issuer is valid for `EnvTest`.

Bugs: none. No separate yes.

Not in this acceptance:

- A config helper that returns durations.
- Ping-on-boot.
- `ReadTimeout`, `WriteTimeout`, or `IdleTimeout`.
- An `ErrorLog` adapter.
- Merging contract fixtures, or a test that signals the process.
- Applying the earlier notes.

## Verification

No production edit. `make verify` was not run. No probe. Shutdown behavior is the existing `TestServeServesProbesAndShutsDown` and plan 1.10, not a new run.
