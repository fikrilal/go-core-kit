# Section note: platform primitives

- Status: Config order and the redact limit shipped in N2. Health probes still open. Nested redaction and `version` still need a yes.
- Date: 2026-10-11
- Scope: `internal/platform/config`, `logging`, `clock`, `health`, `version`
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: config order and redact comment applied 2026-10-11. See `docs/exec-plans/completed/n2-config-redact.md`.
- Previous note: `_WIP/readability/01-platform-httpx.md` is applied.

## Verdict

This set is already close to the mobile bar. Each package has one job. `clock` is the file to copy from: one interface, one production implementation, one test double, and the UTC rule is the function body.

Features do not read the environment and do not call `time.Now()`. `config.Load` is the only `env.Parse`. Service and domain code take a `clock.Clock`. That is the shared-core habit working.

No client-visible bug of the kind in the `httpx` note. Two traps are real and should be fixed only when this note is accepted. Neither is a rewrite.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `config/config.go` | 144 | Env schema, `Load`, `Validate` |
| `config/config_test.go` | 123 | Defaults, parse, required, port, prod logs |
| `logging/logging.go` | 69 | `slog` handler, level, redaction |
| `logging/logging_test.go` | 109 | JSON, text, level filter, redaction, correlation keys |
| `clock/clock.go` | 23 | `Clock`, `System`, `Fixed` |
| `health/health.go` | 72 | `/health`, `/ready` |
| `health/health_test.go` | 118 | 200, 503, no error leak, no access log |
| `version/version.go` | 8 | `Version`, `Commit` |
| `env.example` | 42 | Companion to the env tags |

No test file exists for `clock` or `version`.

## Hard to read

### 1. `Validate` hides which invariant it does not own

`config.go` `ProductionLike` says it reports whether the environment requires real secrets. `Validate` never calls it. A prod process with empty `AUTH_ISSUER`, `AUTH_AUDIENCE`, and signing keys passes `Load`.

The failure happens later, and only if something constructs the keyring and the verifier:

- `internal/platform/auth/auth.go` refuses empty signing keys when `ProductionLike` is set.
- `internal/platform/auth/verifier.go` refuses empty issuer and audience on the same flag.

`cmd/api` does construct both before listen, so today's API process still refuses to start. A reader of `Validate` cannot see that. The comment on `ProductionLike` overclaims. ADR 0007 says `Validate` runs production-required keys. The keys are enforced in another package.

Do not move JWK parsing into `config`. Crypto does not belong in the env file. When this note is accepted, add one sentence on `ProductionLike` naming the two constructors that enforce it. Leave the checks where they are.

### 2. The auth number checks have no stable order

`Validate` walks a `map[string]int` for the six `AUTH_*` integers. Map order is random. Two bad values produce one error, and which name appears is not fixed. Startup still fails. A test cannot assert the full message if more than one field is wrong. No test covers this loop at all.

Mobile habit: the decision list is ordered, so the first failure is the same on every run. Replace the map with a slice of name/value pairs. Same rule (`< 1`), same errors, stable name.

### 3. `/ready` logs on the process logger, not the request logger

`health.go` `handleReady` warns with `h.logger`. The request id lives on the logger `httpx` stored in the request. This handler never reads it.

The access log is skipped on purpose (`SkipAccessLog`, tested). The warning is then the only line for a 503, and it has no `request_id`. The response still echoes `X-Request-Id`. A person holding that id cannot find the check failure.

`h.logger` is used only there. The fix is `httpx.Logger(r).Warn(...)`. The stored logger field can go away if nothing else needs it. Behavior of the HTTP body stays.

### 4. `writeStatus` is a second JSON writer

`health.go` marshals, sets `Content-Type`, and ignores both the marshal error and the write error. `httpx.Document` already writes raw JSON and turns a marshal failure into a problem response.

The body must stay unenveloped. The pinned contract for `GET /health` and `GET /ready` is `{status}`, and backend marks the same routes `@SkipEnvelope`. `httpx.Data` would be wrong. `httpx.Document` is the matching helper.

`statusResponse` is strings and a map, so marshal does not fail today. The duplication is the problem, not a live bad body. One call to `Document` removes the private writer.

## Correct Go that should stay

- `Env`, `LogLevel`, and `LogFormat` are named string types. Each `valid()` method is a switch that lists the legal values and returns false. That is the exhaustive-decision habit. Keep it.
- `Load` is three steps: parse, validate, return. `MustLoad` panics for a composition root. `cmd/api` uses `Load` and prints the error, which is the better caller. `MustLoad` can stay for a root that wants the panic. Do not make `Load` panic.
- Defaults sit on the struct tags (`APP_ENV=dev`, port `8080`, info, JSON). `env.example` matches every tag. Local example uses `LOG_FORMAT=text`. Code default is JSON. Both are legal outside prod. Not a drift bug.
- Prod refuses text logs. Tested.
- Port must be `1..65535`. Tested for `0`. `65536` and a negative port are the same branch and untested. Not a separate fix.
- Auth policy numbers default in the same struct as the port. ADR 0007 asks for typed sub-config at the feature boundary. That already exists: `internal/features/auth.Config` is the slice the feature sees. The flat struct is the env schema. Splitting it now would add types with one caller. Keep one schema until a second process has a different required set. `DATABASE_URL` is still absent. ADR 0007 names it for the Postgres build. There is no `pg` config file yet. Leave it until that build exists.
- `logging.New` is a level, a format, and a writer. The level switch defaults to info. `Validate` already rejects an unknown level before `cmd/api` calls `New`. The default only protects a caller who skips `Validate`.
- Redaction by attribute key is the right shape for `slog`. Keys are lowercased. Groups work: `TestRedaction` puts `access_token` inside `creds` and the secret does not appear. `email` is kept. Correlation constants `request_id` and `trace_id` are the names `httpx` already logs. `FieldTraceID` has no producer yet. That waits for otel, same as the `httpx` note.
- `clock.System.Now` is `time.Now().UTC()` and nothing else. `Fixed` returns the stored moment. Callers in features take the interface. Grep of `time.Now()` outside this file is tests, `httpx` access-log duration, and `tools/gokit`. No feature or domain call. Do not inject a clock into the access log. That was already accepted as out of scope in the `httpx` note.
- Health checks are a name plus a function. The handler does not know Redis or SQLite. `cmd/api` passes the checks in. A failing check becomes the word `unavailable` in JSON and the real error only in the log. `TestReadyWithFailingCheck` locks the non-leak. That split matches the mobile habit of a stable client failure and a separate log of the cause.
- `/health` does not run checks. `/ready` does. Unknown paths are not this package's problem.
- `version.Version` and `version.Commit` are plain vars with safe defaults. The file is readable. It is unused. See below.

## Real bugs

### B1. Redaction does not look inside a value

Probed 2026-10-11. This log:

```go
logger.Info("nested", "payload", map[string]string{"password": "hunter2"})
```

emitted:

```json
{"level":"INFO","msg":"nested","payload":{"password":"hunter2"}}
```

`ReplaceAttr` sees the key `payload`, which is not in the sensitive list, and `slog` then JSON-encodes the map as one value. Nested keys never pass the list. The same hole exists for a struct logged with `slog.Any`.

No current production log does this. Call sites pass strings or `slog.Any("error", err)`. The tests only prove top-level and group keys, so they read as "secrets are redacted" without stating the limit.

This becomes a secret leak on the first nested payload. When accepted: document the limit on `redact`, and reject or walk `KindAny` maps before a feature logs a body. Do not pretend the current key list covers it. A message string that contains a password is also not scanned. Say that in the same comment.

### B2. `version` says the build injects it. Nothing does

`version.go` documents build-time injection. Nothing in the module reads `Version` or `Commit`. The `Makefile` and Go files have no `-X` ldflags. The running binary cannot report a revision because nothing asks.

Not a behavior bug in the two assignments. The comment is false today. When accepted, either wire ldflags and log the pair from `cmd/api`, or drop the "injected at build time" sentence until a caller exists. Deleting the package is a separate yes. Do not delete it inside a comment fix.

## Design question

Which mechanic is still in features, and should it move here?

Auth already receives a typed `Config` and a `clock.Clock`. It does not parse env. Password length, token TTL, and login limits are platform fields with one feature consumer. That is correct. The translation from `config.Config` ints into `time.Duration` sits in `cmd/api` / `auth.Register`, which is the composition root. Leave it there. A helper inside `config` called `AuthConfig()` would be a second type for one caller.

Do not add health knowledge of Redis or SQLite inside this package. The check function is the extension point.

Do not add a logging facade with `Info` / `Error` methods. `slog` is the facade. Mobile's `Log` class exists because Flutter had no single logger. Wrapping `slog` again would hide the API Go developers already know.

`httpx` should keep owning request id and access log. These packages should keep owning env, log setup, time, and probes.

## Function notes

### `config.go`

- Enum types and consts. Clean.
- `Config` struct. Clean as a schema. Auth fields are a block under the platform fields. `env.example` documents the staging/prod requirement that `Validate` does not enforce (finding 1).
- `ProductionLike`. Finding 1. The bool itself is the right API. Callers in `server.go` pass it through.
- `Load`. Clean.
- `MustLoad`. Clean. Untested. Low value until a root uses it. `main` does not.
- `Validate`. Findings 1 and 2. Enum and port and prod-log checks are clear `if`s with the env name in the error. Keep the error strings. Tests match on `DATABASE_PATH`, `HTTPPort`, `APP_ENV`, `HTTP_PORT`, `LOG_FORMAT`.
- `valid` methods. Clean.

### `config_test.go`

Covers defaults, a parsed override, missing `DATABASE_PATH`, a bad port number, `APP_ENV=wat`, port `0`, and prod+text. Uses `t.Setenv` except `unsetenv`, which has to clear a key that `t.Setenv` cannot remove. That helper is longer than the tests and correct.

Missing, not bugs: auth integers below 1, port above 65535, bad `LOG_LEVEL`, bad `LOG_FORMAT`, `ProductionLike` true for staging and prod and false for dev and test.

### `logging.go`

- Field constants and `sensitiveKeys`. Clean. The list is snake_case words: `password`, `token`, `access_token`, `refresh_token`, `id_token`, `authorization`, `cookie`, `secret`, `api_key`, `apikey`, `private_key`. `Authorization` matches because the key is lowercased. `accessToken` does not match `access_token`. Worth one example in the comment next to bug B1, not a new matcher.
- `New`. Clean.
- `slogLevel`. Clean switch.
- `redact`. Bug B1. The function is four lines and easy to misread as deep.

### `logging_test.go`

JSON, text, level filter, redaction, and bound request/trace ids. The redaction test is the right idea and stops at attribute keys. It does not cover bug B1.

### `clock.go`

Clean. No test. `Fixed` is small enough that a test would only repeat the return. `System` would assert `Location() == time.UTC`, which is the one assertion worth having if this file is ever touched. Not required to believe it.

`Fixed.Moment` is exported and mutable. Tests can set it. Sharing one `Fixed` across goroutines and writing `Moment` is a race. No production code holds a `Fixed`. Test clocks stay single-threaded. Do not add a mutex.

A zero `Fixed` returns the zero time. Production wiring uses `System`. A test that forgets `Moment` will see 1970. Acceptable for a test double.

### `health.go`

- `Check`, `Handler`, `New`, `Register`. Clean. Operation ids `health.get` and `ready.get` match the pinned contract.
- `handleHealth`. Clean. Ignores the request after the signature. Liveness must not touch dependencies.
- `handleReady`. Finding 3. The loop is readable: run check, record the public word, log the cause, then 503 or 200. A nil `Verify` panics and the `httpx` recover turns the probe into 500. No caller passes nil. Residual.
- `statusResponse`. Clean. `checks` omitted when empty, so the 200 body is `{"status":"ok"}`.
- `writeStatus`. Finding 4.

The pinned `/ready` schema documents 200 only. This handler returns 503 with a `checks` object when a dependency is down. Backend's controller returns `{status:"ok"}` after `assertReady()` and does not document 503 in the same schema. The 503 is what a probe needs. Not a readability defect. A contract-suite fight belongs with the contract section, not a rewrite of this function.

### `health_test.go`

Clean helpers. Covers liveness headers and body, ready success, ready failure without leaking `connection refused`, and empty access logs for both probes. Does not assert `request_id` on the warning. That gap hides finding 3.

### `version.go`

Bug B2. Eight lines, nothing to simplify.

## What acceptance authorizes

Taste and the small traps, no HTTP shape change:

1. Point `ProductionLike` at the auth constructors that enforce secrets. Do not move those checks.
2. Make the auth integer checks an ordered list.
3. Log readiness failures with `httpx.Logger(r)`.
4. Replace `writeStatus` with `httpx.Document`.
5. State on `redact` that only attribute keys are covered, including the nested-map case from B1.

Separate yes:

- Walk map values in `redact`, or ban `KindAny` for sensitive payloads. That changes log output.
- Wire `version` into the binary and a startup log, or delete the package.

Not in this acceptance:

- Splitting `config.Config` into per-feature env structs.
- A logging wrapper around `slog`.
- Postgres `DATABASE_URL`.
- Applying the `httpx` note.

## Verification

No production edit. `make verify` was not run.

Probe, deleted afterward (`internal/platform/logging/probe_audit_test.go` is not in the tree):

```bash
go test ./internal/platform/logging -run TestAuditRedactProbe -count=1
```

Result: pass. The logged line is quoted in B1. The probe is not coverage.
