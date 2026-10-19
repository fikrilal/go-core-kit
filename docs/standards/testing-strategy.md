# Testing strategy (normative)

Tests run inside `make verify` (`go test ./...` in both the fast and full profiles). No test may need the network or a live external service: boot the real composition against a temp SQLite database and miniredis.

## Layers

| Layer | What it proves | Where |
| --- | --- | --- |
| Unit | Pure logic: parsers, middleware behavior, helpers | package tests |
| Integration | The composition boots and serves: probes, shutdown, feature flows | `cmd/api` tests |
| Contract | Live responses match the pinned snapshot and client-observed behaviors | `cmd/api` tests via `contracttest` |

## Contract suite

The harness validates live responses against `contracts/backend.openapi.yaml`:

- `suite := contracttest.New(t)` loads and validates the pinned snapshot.
- `contracttest.Do(t, client, req)` performs a request; the returned response value carries status, header, and the fully read body.
- `suite.ValidateResponse(t, req, resp)` validates status, headers, and body against the operation schema; undeclared statuses fail.
- `contracttest.AssertRequestID(t, resp)` requires a well-formed `X-Request-Id` echo.
- `contracttest.AssertProblem(t, resp, "CODE")` requires RFC7807 shape, the exact code, and a trace id equal to the request id.
- `suite.AssertErrorCodeDeclared(t, operationID, code)` checks membership in the operation's `x-error-codes`.

The pinned snapshot declares success schemas only: happy-path responses validate against it, while problem responses assert the platform shape, the exact status, and declared error codes.

`CONTRACT_SNAPSHOT_PATH` overrides the snapshot location (gate-honesty injections use it).

## Per-operation expectations

- Every implemented operation has at least one live contract test; auth flows additionally assert rotation/reuse semantics.
- Tests exercise the real middleware chain through `newServer`, never a reduced router.
- Responses with a declared status validate against the snapshot; a failure path without a declared status asserts the platform shape and the gap stays visible in the snapshot.

## Golden response shapes

`cmd/api/testdata/golden/<operation>.json` captures each wired operation's wire
*shape*: the status, the envelope's top-level keys, and the problem code for a
refusal. Values are deliberately absent — this is a shape oracle, so ordinary data
changes do not churn it, while a changed envelope, a lost `meta` block, or a swapped
error code fails with a diff naming the operation.

Regenerate after a deliberate contract change:

```bash
go test ./cmd/api -update-golden
```

The captures are reviewed like any other contract edit, which is the point: a wire
change arrives as a diff against the shape it replaces, not as a silent update.
Every response the contract suite validates is captured: 25 call sites across the
suite, keyed by route path with generated segments normalized to `_id_`, so a UUID in
a path does not mint a new file per run.


## The job-outcome oracle

`internal/features/auth/oracle` holds the account-state matrix the email jobs are
asserted against — the states a job can meet, the outcome each must report, and
whether it may send anything. It is data, not logic, and it lives in a small
non-test package because a `_test` file cannot be imported by another test package
(the same trade `internal/platform/dbtest` makes).

Both levels read the same table: the feature's tests assert the *outcomes* the
handlers report, and the worker's test asserts the *effects* the queue cares about —
an email leaves only for the states the matrix says send, and a token is minted only
then. Adding a skip is a row in the oracle plus whatever either level needs to seed
it, and a rename of an outcome fails in both places rather than in one.
