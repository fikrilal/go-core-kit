# R1 — httpx taste

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 01
- Related: `_WIP/readability/01-platform-httpx.md`

## Objective

Make `internal/platform/httpx` readable without changing a client-visible response. Unnest the middleware chain, split idempotency replay so a bool does not mean both "replayed" and "already failed", and make the lock-release test look at the key the handler writes.

## Constraints

- No status, code, header, or body change.
- Idempotency key shape stays `idempotency:<operation id>:<header>`.
- Auth and idempotency stay per route. Do not put them in `NewRouter`.
- B1, B2, and B3 stay out. Plans R2 and R3 own them.
- Do not move the auth `ProblemError` table, merge the two response writers, inject a clock into the access log, or hide decode-then-`WriteError`.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `NewRouter` assigns the chain in execution order: body limit, panic recovery, access log, request id. A comment says auth and idempotency wrap single routes, guard outside.
2. Loading a cached idempotency record does not write the response. The caller writes the 500 or the replay.
3. `Document` calls `writeJSON`.
4. Comments exist on the second `Decode`, on `releaseLock`'s `WithoutCancel`, and on `filterKeyPattern`. The validator tag function is named `jsonFieldName`.
5. `TestIdempotencyReleasesLock` uses `testOp(...).ID`. A lock left under that key fails the test.

## Steps

- [x] Unnest `NewRouter` and add the route-wrapper comment.
- [x] Replace `tryReplay` with `loadCached` plus `writeCached`.
- [x] Point `Document` at `writeJSON`.
- [x] Add the three comments and name `jsonFieldName`.
- [x] Fix the lock-key assertion.
- [x] `go test ./internal/platform/httpx` and `make verify`.

## Decision Log

- 2026-10-11: B4 rides in this plan. The test reports green for a leaked lock. Fixing the assertion does not change production behavior.
- 2026-10-11: The loader cannot be named `cachedResponse`. That identifier is already the stored record type. The loader is `loadCached`.

## Verification

```bash
go test ./internal/platform/httpx -count=1
make verify
```

Outcome (2026-10-11): `go test ./internal/platform/httpx -count=1` passed. `make verify` (full profile) passed in 21.329s: knowledge, contract, fmt-check, vet, lint (0 issues), test, build, build-pg, drift, contract-lint, gates-honesty.

## Completion Notes

Shipped the taste edits in `internal/platform/httpx` with no client-visible response change. `NewRouter` assigns body limit, panic recovery, access log, then request id, and comments that auth and idempotency wrap single routes. `loadCached` only reads; `writeCached` only writes the replay; both `handle` sites write the 500 themselves. `Document` calls `writeJSON`. Comments cover the second `Decode`, `releaseLock`'s `WithoutCancel`, and `filterKeyPattern`. The validator tag function is `jsonFieldName`. `TestIdempotencyReleasesLock` asserts `redis.Key("idempotency", op.ID, "key-1") + ":lock"`. Idempotency key shape is unchanged. B1, B2, and B3 were not touched.

## Follow-Ups

- [ ] R2 problem details (B1, B3)
- [ ] R3 idempotency caller scope (B2)
