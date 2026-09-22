# Section note: `internal/platform/httpx`

- Status: Taste, B4, B3, and B2 shipped. B1 blocked on the contract.
- Date: 2026-10-11
- Scope: every hand-written file in `internal/platform/httpx` (8 production files, 8 test files)
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: R1, R2, and R3 applied 2026-10-11. See `docs/exec-plans/completed/r1-httpx-taste.md`, `docs/exec-plans/completed/r2-httpx-problem-details.md`, and `docs/exec-plans/completed/r3-httpx-idempotency-scope.md`.

## Verdict

Handlers already get the mobile shape. `Data`, `List`, `Created`, `NoContent`, `Decode`, `ParseListQuery`, and `WriteError` are short, named, and do one job. `auth.go` handlers read top to bottom because this package exists. That part of the bar is met. Do not wrap those calls in another helper.

The hard reading is inside the package, in two places:

- `NewRouter` hides middleware order inside one nested call.
- `idempotencyStore.tryReplay` both writes the response and returns a bool that does not mean "replayed".

Three behavior bugs are real and separate from taste. One test is green without proving its claim. None of them should be folded into a cosmetic edit.

## What was read

Production:

| File | Lines | Role |
| --- | --- | --- |
| `codes.go` | 33 | Wire error codes |
| `decode.go` | 80 | JSON body + `validate` tags |
| `problem.go` | 75 | `ProblemError` to RFC7807 |
| `respond.go` | 71 | Envelope and document writers |
| `router.go` | 74 | Route table and middleware entry |
| `middleware.go` | 124 | Request id, access log, panic, body limit |
| `idempotency.go` | 163 | `Idempotency-Key` lock and replay |
| `listquery.go` | 219 | `limit`, `cursor`, `sort`, `filter[]`, `q` |

Tests: `decode_test.go`, `problem_test.go`, `respond_test.go`, `middleware_test.go`, `operation_test.go`, `helpers_test.go`, `listquery_test.go`, `idempotency_test.go`. External package `httpx_test`, so they only touch the public API. That should stay.

Imports, package comments, and struct tags were read. They are mentioned below only where they affect a finding.

## Hard to read

### 1. Middleware order is one nested expression

`router.go` `NewRouter`:

```go
handler: requestID(accessLog(logger, recoverPanics(bodyLimit(mux)))),
```

Outer wrapper runs first. The actual order is request id, access log, panic recovery, body limit, then the mux. A reader has to expand four functions and reverse them. ADR 0003 also lists auth and idempotency in this chain. In code those two are per route (`guard.Require` outside `Idempotent` in `internal/features/auth/auth.go`). The expression does not say that.

Mobile habit: each step is a named line, in the order it runs. Proposed edit, no behavior change:

```go
handler := bodyLimit(mux)
handler = recoverPanics(handler)
handler = accessLog(logger, handler)
handler = requestID(handler)
```

Add one comment that auth and idempotency are not in this chain. They wrap individual routes, guard outside idempotency.

### 2. `tryReplay` hides the decision

`idempotency.go` `tryReplay` returns `true` in three different cases:

- Redis `Get` failed, and it already wrote a 500.
- The cached JSON is corrupt, and it already wrote a 500.
- A cached 2xx was written, with `Idempotency-Replayed: true`.

It returns `false` only for a miss. Both callers are `if s.tryReplay(...) { return }`. The name says "replayed". The true branch also means "this request already failed closed". The next edit can treat a Redis outage as a successful replay.

Mobile habit: the helper returns the outcome. The caller decides. `AuthRepositoryImpl.login` does not write HTTP, and `mapAuthFailure` does not return a bool that means two things.

Proposed shape, same behavior:

```go
found, err := s.cachedResponse(r.Context(), responseKey)
if err != nil {
    WriteError(w, r, err)
    return
}
if found {
    writeCached(w, foundResponse)
    return
}
```

`cachedResponse` does not touch `http.ResponseWriter`. `writeCached` is the only replay writer.

### 3. `Document` repeats `writeJSON`

`respond.go` `Document` and `writeJSON` marshal, turn a marshal error into `WriteError`, and call `writeBody`. `Document` is the non-envelope writer. The comment on `Document` is the useful part. The body should be `writeJSON(w, r, http.StatusOK, v)`.

### 4. Comments missing where the line is not obvious

These stay as code. They need one line each, because the reason is a policy a junior cannot recover from the call:

- `decode.go` `Decode`: the second `dec.Decode(&struct{}{})` exists to reject a second JSON value after a valid body. `DisallowUnknownFields` does not do that job.
- `idempotency.go` `releaseLock`: `context.WithoutCancel` is there so a disconnected client still deletes the lock. Plan 1.8 recorded that decision. The code does not.
- `listquery.go` `filterKeyPattern`: the expression accepts `filter[field]` and `filter[field][op]`. The op group is lowercase only, so `filter[status][GTE]` is "not a valid filter", not "operator not allowed".

### 5. `newValidator` tag function is an unnamed closure

`decode.go` lines 32–37. Official `validator` snippet, correct, dense. Name it `jsonFieldName` so the `json:"-"` branch has a place to sit. No behavior change.

## Correct Go that should stay

- `Code` is a named string type. Global codes live next to the renderer that emits them. The repeated `//nolint:gosec` on token and password code names is the lint exception, not noise to delete. Those identifiers are wire codes. `gosec` would treat them as secrets.
- `validationMessage` is an exhaustive switch with a default `"is invalid"`. This is the habit we want.
- `ParseListQuery` is the best file in the package. It builds a parser, calls `parseLimit`, `parseSort`, `parseFilters`, `parseSearch`, and returns one `ProblemError` if any issue was collected. Each parser method does one field. Unknown non-filter query keys are ignored. That matches a list URL, where extra analytics keys must not fail the request.
- `writeJSON` / `writeBody` ignore the `Write` error. The connection is gone. Wrapping it does not give the handler a second chance. The `wrapcheck` nolint on `statusWriter.Write` and `recordingWriter.Write` is the same decision.
- `WriteError` maps a non-`ProblemError` to 500 `INTERNAL`, logs the original, and does not copy it into `detail`. `TestWriteErrorUnknownIsInternalAndLogged` locks that. Keep it.
- `writeProblem` drops the `json.Marshal` error (`body, _`). A problem body is strings and ints. Marshal failure there is a bug in this package, and calling `WriteError` from inside `writeProblem` can recurse. The blank identifier is the right call. A five-word comment is enough if someone flags it later.
- `accessLog` calls `time.Now()`. The clock rule targets service and domain code. An access-log duration is the middleware's own measurement.
- `HandleIdempotent` panics when `WithIdempotency` was not set. That is startup wiring, and `TestHandleIdempotentRequiresConfiguration` expects the panic. A returned error would only move the failure into every call site.
- `statusWriter` and `recordingWriter` look alike. Leave them apart. One records status for the log. The other also buffers the body for Redis. A shared "writer with options" would be the speculative abstraction the proposal rejects.
- `RequestID` and `Logger` use unexported context keys. Callers cannot collide with them.
- `SkipAccessLog` is a concrete-type assertion on `*statusWriter`. Health probes call it on the writer this chain passes in, and `TestSkipAccessLogMarksRequest` covers that. It is a silent no-op if a wrapper sits in front. No caller does that today. Do not replace it with a context flag unless a real wrapper appears.
- `router.Operations` returns `slices.Clone`. Callers cannot mutate the route table.
- Test helpers `testRouter`, `testOp`, and `problemResponse` are shared, small, and in `httpx_test`. Keep tests outside the production package.
- Missing `Idempotency-Key` passes through. Only 2xx is stored. Concurrent duplicate returns 409 `IDEMPOTENCY_IN_PROGRESS` immediately, with no wait loop. Plan 1.8 decided all three. Backend waits and hashes the body. That difference is a recorded follow-up (payload fingerprint), not an accident to "clean up" in a rename.

## Real bugs

These change what a client observes, or they make a test lie. Accept them separately from the taste list.

### B1. Wrong method on a known path is 404

Probed 2026-10-11 with `GET /v1/auth/password/login` while only `POST` was registered:

- status `404`
- `Content-Type: application/problem+json`
- `Allow` empty
- body code `NOT_FOUND`

Cause: `NewRouter` registers `mux.HandleFunc("/", notFound)`. That pattern matches every path and every method. ServeMux only prefers `POST /v1/auth/password/login` when the method matches. A `GET` falls through to the catch-all.

Plan 1.3 follow-up says 405 still returns stdlib plain text. That note is stale. The catch-all now swallows the method mismatch, so the client cannot tell "no such route" from "wrong verb".

Fix, when accepted as a bug: if the path is registered for another method, write a 405 problem with `Allow`. Leave unknown paths on the current 404 problem. Add a test next to `TestNotFoundUsesProblemDetails`.

### B2. Idempotency cache is not scoped to the caller

`Idempotent` uses `op.ID` plus the raw header as the Redis key (`idempotency:<operation id>:<header>`). It does not include the authenticated user, and it does not hash the body.

The only production use is change-password:

```go
router.Handle(changePassword, guard.Require(router.Idempotent(changePassword, handlers.changePassword)))
```

The guard stops anonymous callers. It does not stop two logged-in users who send the same `Idempotency-Key` from sharing one cache entry. The second user receives the first user's `204` and the handler does not run. Their password does not change.

Backend key shape, `libs/platform/http/idempotency/idempotency.service.ts`, is `idempotency:v1:<userId>:<scopeHash>:<header>`. Same key with a different body is `409 CONFLICT`. Header length above 128 is `400`. Plan 1.8 explicitly deferred payload fingerprinting. It did not defer user scope, and its impact line says "Auth / session: no", which was before this route existed.

The comment on `Idempotent` ("compose it inside guards so unauthorized requests never touch the replay store") is true and incomplete. A reader can believe the guard makes the cache safe.

Also, `recordingWriter.Write` forwards bytes to the client immediately, then `store` runs. A Redis `Set` error is logged and the client keeps the success. Plan 1.8 says a store failure fails the request. That sentence cannot be true with write-through. A lost record means the retry runs the handler again. Change-password then tends to fail the current-password check. A later create route would duplicate the write.

Do not copy `HandleIdempotent` onto another route until user scope is decided. Payload hash stays the existing follow-up, not a new surprise.

### B3. A too-large body is reported as bad JSON

`bodyLimit` wraps the body with `http.MaxBytesReader` at 1 MiB. `Decode` turns every decoder error into `invalidBody()`, detail `"invalid JSON body"`.

Probed with a JSON object whose `name` is `1 << 20` bytes:

- status `400`
- code `VALIDATION_FAILED`
- detail `invalid JSON body`

`TestRequestBodyLimit` only asserts status 400, so the lying detail is untested. Empty body returning that same detail is correct: there is no JSON value. The size failure is a different cause and should say so. Keep `VALIDATION_FAILED` unless the contract already has a distinct code. I did not find a separate code in `codes.go`.

### B4. The lock-release test checks a key that is never written

`TestIdempotencyReleasesLock` builds:

```go
redis.Key("idempotency", "POST /v1/things", "key-1") + ":lock"
```

`testOp` sets `ID` to `"test:POST:/v1/things"`. `wrap` scopes by `op.ID`, so the real lock is:

```text
idempotency:test:POST:/v1/things:key-1:lock
```

`miniredis.Exists` on the old pattern key is false even if the real lock leaked. Plan 2.4 moved scope from the route pattern to the operation id. This assertion was left on the old layout. Acceptance criterion 5 of plan 1.8 is not proven.

Fix: build the key from the same operation id `HandleIdempotent` receives. No production change.

## Design question

Which mechanic is still in features, and should it move into `httpx`?

`Service.Register` and the helpers at the bottom of `internal/features/auth/app/service.go` build `*httpx.ProblemError` directly (email exists, validation, invalid credentials, refresh reuse, OIDC, and the rest). The use case knows status codes. Mobile does not do this. `LoginUserUseCase` returns `AuthFailure`. `mapAuthFailure` in `lib/features/auth/data/error/auth_failure_mapper.dart` is the translation table, and the repository only calls `mapLeft`.

Do not move that table into `httpx`. Platform would then know every auth outcome, which is how `codes.go` already grew an auth catalog beside `VALIDATION_FAILED` and `INTERNAL`. The renderer stays here. The mapping belongs to the auth feature note, in one function whose only job is domain error to `ProblemError`, same rule as the mobile mapper file: a real table gets a file, a one-line map stays inline.

Auth constants can stay typed as `httpx.Code` and move next to that mapper. Moving them is an auth-section edit, not an `httpx` cleanup. `ProblemError.Code` stays a `Code` so features cannot invent a raw string without meaning to.

What `httpx` should keep owning, because every feature would otherwise rebuild it:

- envelope and problem JSON
- request id and access log
- decode and the validation-message switch
- list query parsing
- idempotency storage

What features should keep owning:

- which status and code a domain outcome uses
- whether a route is public, guarded, or idempotent
- the allowlist inside `ListQueryConfig`

The handler skeleton in `auth.go` (decode, `WriteError`, call service, `Data` or `NoContent`) is the readable part. Leave it visible.

## Function notes

Clean means no finding beyond the buckets above.

### `codes.go`

- `Code` and the global const block. Clean.
- Auth const block. Ownership issue, recorded in the design answer. Not a syntax problem. File is 33 lines and grouped.

### `decode.go`

- `Decode`. Sequence is body, single value, struct tags. Second decode needs the comment in finding 4. All JSON failures share `invalidBody` (bug B3 for the size case).
- `newValidator`, `validateStruct`. Clean aside from the unnamed tag function (finding 5). A non-struct target becomes `fmt.Errorf("validate: ...")`, and `WriteError` renders that as 500. That is a programmer error, not a client case.
- `validationMessage`. Clean. `min` and `gte` share "must be at least N". For a string, N is length. The message is a bit blunt and still honest.
- `invalidBody`. Clean. Used for syntax, unknown field, trailing value, empty body, and today the size limit.

### `problem.go`

- `FieldIssue`, `ProblemError`, `Error`. Clean. `Error()` returns `Detail` when set, otherwise the code. Logs of an expected failure show the client sentence, not a stack.
- `problem` struct. Matches the contract fields this phase owns. `otelTraceId` is still absent. Plan 1.3 follow-up, waiting on the otel package.
- `WriteError`. Clean. See the stay list.
- `writeProblem`. Title is `http.StatusText`, except `VALIDATION_FAILED` which is `"Validation Failed"`, and an unknown status number becomes `"Error"`. Two `if`s, not a switch. Short enough to stay. `Retry-After` is set only when `RetryAfterSeconds > 0`.

### `respond.go`

- `envelope`, `ListMeta`. Clean. `nextCursor` omitted when empty. Tested.
- `Data`, `List`, `Created`, `NoContent`. Clean. This is the feature-facing API.
- `Document`. Finding 3. Behavior is the right exception: JWKS is not an envelope. Tested.
- `writeJSON`, `writeBody`. Clean. `Created` sets `Location` before marshal. If marshal failed, the 500 problem would still carry `Location`. No current body fails marshal. Residual, not worth a guard.

### `router.go`

- `Operation`, `pattern`, `Handle`, `HandleFunc`, `register`, `Operations`, `ServeHTTP`. Clean. `HandleIdempotent` lives in `idempotency.go` and calls `Handle`.
- `NewRouter`. Finding 1, and bug B1 via the `"/"` registration.
- `notFound`. Clean for unknown paths.
- `RouterOption`. Today the only option stores a Redis client. It does not wrap `handler`. The name reads like middleware. Fine while one option exists. Do not invent a middleware-option framework.

### `middleware.go`

- Context keys, `RequestID`, `Logger`. Clean. Missing request id yields `""`.
- `requestID`. Echoes any client value, or generates a UUID string. Contract asks for echo. No length cap. A newline would be escaped by the JSON logger. Not a finding.
- `accessLog`. Clean. Status `>= 500` logs at error, everything else at info, including 404. Quiet flag skips the line. `time.Now` stays.
- `recoverPanics`. Logs panic and stack, writes 500 `INTERNAL`. If the handler already wrote bytes, the problem JSON is appended to that body. Residual. No current handler writes and then panics.
- `bodyLimit`. Constant `1 << 20` with a name. The misleading decode detail is bug B3, not this function.
- `statusWriter`. Clean. First `Write` implies 200. Later `WriteHeader` is ignored. Same rule as `net/http`.

### `idempotency.go`

- `WithIdempotency`, `Idempotent`, `HandleIdempotent`. Panic-on-miswire stays. Comment overclaim is bug B2.
- `cachedResponse` fields. Stores status, content type, location, body. Other response headers are dropped on replay. No current handler sets another header that clients need. Residual.
- `wrap`. Missing header calls the handler. Stays.
- `handle`. The sequence itself is readable: replay, lock, replay again, 409, run, store. The `tryReplay` bool is finding 2. User scope and write-through are bug B2. Lock TTL is 30 seconds and is not refreshed. Plan 1.8 calls that crash recovery. A handler longer than 30 seconds can run twice. No current auth handler approaches that.
- `tryReplay`. Finding 2. Corrupt JSON writes 500 and keeps the key until the 24 hour TTL, so every retry 500s. Rare. Worth remembering when the function is split, not a separate fix.
- `store`. Skips non-2xx. Marshal error ignored, same reasoning as `writeProblem`. Redis `Set` error logged only (bug B2).
- `releaseLock`. Behavior stays. Comment missing (finding 4).
- `recordingWriter`. Stays separate from `statusWriter`. Write-through is the bug, not the duplication.

### `listquery.go`

- Defaults, `ListQuery`, `SortField`, `FilterOp`, configs. Clean.
- `ParseListQuery` and `limitDefault` / `maxLimit`. Clean for real configs. `DefaultLimit` is not clamped to `MaxLimit`, and the default sort is not checked against `Allowed`. No production caller exists yet. Do not add a second validator until a caller can set a bad config.
- `parseLimit`. Clean. Non-integer, `< 1`, and `> max` each add one issue.
- `parseSort`. More than `MaxSortFields` (3) is one issue and returns before field checks. Unknown fields each add an issue and parsing continues, but `ParseListQuery` still fails the request, so the partial sort is discarded. Clean.
- `parseFilters`, `parseFilter`, `filterValues`. Clean. Keys are sorted so issue order is stable. Repeated key is rejected. `in` splits on comma and trims. A value that itself contains a comma cannot be expressed. No current filter needs that.
- `parseSearch`. Clean. `q` is rejected when `Search` is false, ignored when absent.

## Test notes

Covered well: envelope shape, problem shape, request id echo and generate, access log fields, panic body, 404 problem, skip access log, decode tag messages, unknown JSON field, trailing JSON, list query happy path and the main rejects, idempotency replay, distinct keys, missing key, in-progress 409, error not cached, panic when Redis is not configured.

Gaps that hide the bugs above:

- No test that a registered path with the wrong method is 405. `TestNotFoundUsesProblemDetails` uses a path with no route, so it stays valid.
- `TestRequestBodyLimit` does not read `detail`.
- `TestIdempotencyReleasesLock` asserts the pre-2.4 key. Bug B4.
- No test that two principals with one header do not share a replay.
- No test that a Redis `Get` or `SetNX` error skips the handler.
- No test that `VALIDATION_FAILED` uses the title `"Validation Failed"`. The branch in `writeProblem` is untested. Low, and not a bug by itself.

`decode_test.go` `decode` wraps the error with `fmt.Errorf("decode: %w", err)`. `errors.As` still finds `ProblemError`. Slightly noisier than returning the error unchanged. Not worth a change on its own.

## What acceptance authorizes

Taste, behavior unchanged:

1. Unnest `NewRouter` and comment that auth and idempotency are per route.
2. Split `tryReplay` into "load cache" and "write replay".
3. Make `Document` call `writeJSON`.
4. Add the three comments in finding 4, and name `jsonFieldName`.
5. Fix `TestIdempotencyReleasesLock` to use the operation id. This is bug B4. It belongs in the same pass because the current test reports green for a leak.

Bugs, each changes client-visible behavior, each needs its own yes:

- B1 method mismatch returns 405 problem plus `Allow`.
- B2 scope the idempotency key by principal before any second idempotent route. Decide payload hash there too, or keep the plan 1.8 deferral in writing on the function.
- B3 distinct detail for `http.MaxBytesError`.

Not in this acceptance:

- Moving auth codes or the `ProblemError` table out of `service.go`. That is the auth-feature note.
- Merging the two response writers.
- Injecting a clock into the access log.
- A helper that hides decode-then-`WriteError`.

## Verification

Audit probes below ran before any edit. R1 landed the taste list and B4. R2 landed the oversized-body detail and stopped B1 because the snapshot has no 405 response and no method-not-allowed code. R3 scopes idempotency keys by caller id. Evidence is in the three completed plans under `docs/exec-plans/completed/`.

Probe, deleted afterward (`internal/platform/httpx/probe_audit_test.go` is not part of the tree):

```bash
go test ./internal/platform/httpx -run TestAuditProbe -count=1
```

Result: pass. Printed observations are quoted in B1 and B3. The probe existed only to see the status line. It is not coverage.
