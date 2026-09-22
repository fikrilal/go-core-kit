# R2 — httpx problem details

- Status: completed
- Owner: Core kit maintainers
- Risk: medium (client-visible errors)
- Date: 2026-10-11
- Roadmap: readability track, note 01
- Related: `_WIP/readability/01-platform-httpx.md`

## Objective

Stop two httpx failures from wearing the wrong problem. An oversized body must not say "invalid JSON body". A known path called with the wrong method must not say `NOT_FOUND`.

## Constraints

- Do this only after R1 is complete.
- B3 keeps `400` and `VALIDATION_FAILED`. Only `detail` changes, and only for `http.MaxBytesError`. Empty and malformed JSON stay "invalid JSON body".
- B1 writes `405` with `Allow` listing the methods registered for that path. A path with no registration stays the current `404` `NOT_FOUND` problem.
- If the pinned snapshot cannot represent 405 for an implemented operation, stop B1 and record the contract gap. Do not invent an error code. Ship B3 anyway.
- No idempotency key change. No payload hash. No auth mapper move.

## Impact Areas

- API / contract: yes (405 and the oversized-body detail)
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. A JSON body over 1 MiB returns `400` `VALIDATION_FAILED` with a detail that says the body is too large. `TestRequestBodyLimit` asserts that detail.
2. `GET /v1/auth/password/login` while only `POST` is registered returns `405`, `Allow: POST`, and a problem body. `Allow` lists every method registered on that path.
3. `GET /v1/missing` stays `404` `NOT_FOUND` with an empty `Allow`.
4. Contract tests still pass. A snapshot gap blocks only the 405 assertion, not the body-size detail.

## Steps

- [x] Teach `Decode` to recognize `*http.MaxBytesError` before the generic invalid-body result.
- [x] Stop B1. The pinned snapshot has no `405` response and no method-not-allowed code. Wrong-method requests stay `404` `NOT_FOUND`.
- [x] `TestRequestBodyLimit` asserts the oversized-body detail. The 405 test was not added.
- [x] Run `go test ./internal/platform/httpx ./cmd/api` and `make verify`.

## Decision Log

- 2026-10-11: B1 and B3 share a plan because both are problem-body corrections and neither touches Redis. They ship as separate steps so a contract stop on 405 does not revert the body-size detail.
- 2026-10-11: B1 stopped. `contracts/backend.openapi.yaml` contains no `405` response. `auth.password.login` declares `VALIDATION_FAILED`, `AUTH_INVALID_CREDENTIALS`, `AUTH_USER_SUSPENDED`, `RATE_LIMITED`, and `INTERNAL`. `httpx.Code` has no method-not-allowed constant. backend-core-kit `defaultProblemCode` maps an unlisted 4xx, including 405, to `VALIDATION_FAILED`. A new code would be an invented wire contract. Wrong-method behavior stays the current 404.

## Verification

```bash
go test ./internal/platform/httpx ./cmd/api -count=1
make verify
```

Outcome (2026-10-11): both commands passed. `make verify` full profile passed in 15.801s. Contract tests still pass. No 405 assertion was added.

## Completion Notes

B3 shipped. A body that trips `*http.MaxBytesError` is still `400` `VALIDATION_FAILED`, with detail `request body is too large`. Empty and malformed JSON stay `invalid JSON body`. `TestRequestBodyLimit` asserts the code and the detail.

B1 did not ship. The snapshot cannot represent 405 for an implemented operation without a new error code. `GET` on a path registered only for `POST` remains `404` `NOT_FOUND` with an empty `Allow`. Unknown paths stay on that same 404.

## Follow-Ups

- [ ] 405 with `Allow` once backend-core-kit publishes a method-not-allowed code and a 405 response. Do not invent the code here.
- [ ] R3 idempotency caller scope
