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
- `suite.ValidateResponse(req, resp)` validates status, headers, and body against the operation schema; undeclared statuses fail.
- `contracttest.AssertRequestID(t, resp)` requires a well-formed `X-Request-Id` echo.
- `contracttest.AssertProblem(t, resp, "CODE")` requires RFC7807 shape, the exact code, and a trace id equal to the request id.
- `suite.AssertErrorCodeDeclared(operationID, code)` checks membership in the operation's `x-error-codes`.

`CONTRACT_SNAPSHOT_PATH` overrides the snapshot location (gate-honesty injections use it).

## Per-operation expectations

- Every implemented operation has at least one live contract test; auth flows additionally assert rotation/reuse semantics.
- Tests exercise the real middleware chain through `newServer`, never a reduced router.
- Responses with a declared status validate against the snapshot; a failure path without a declared status asserts the platform shape and the gap stays visible in the snapshot.
