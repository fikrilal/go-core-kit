# R3 — httpx idempotency caller scope

- Status: active
- Owner: Core kit maintainers
- Risk: high (write replay can cross users)
- Date: 2026-10-11
- Roadmap: readability track, note 01
- Related: `_WIP/readability/01-platform-httpx.md`, plan 1.8

## Objective

Stop two authenticated callers who reuse one `Idempotency-Key` from sharing a cached response. The only production route is change-password. Today the Redis key is `idempotency:<operation id>:<header>`. The second user can receive the first user's `204` and skip the handler.

## Constraints

- Do this only after R1. R2 is independent.
- `httpx` must not import `internal/platform/auth`. That package already imports `httpx`. A resolver registered from the auth feature reads `platformauth.PrincipalFrom` and supplies the user id.
- Key shape becomes `idempotency:<operation id>:<user id>:<header>`. No other prefix change.
- A missing principal does not fall back to a shared key. Fail the request and do not run the handler.
- Payload fingerprint stays deferred. Say that on the function. Do not reject same key with a different body in this plan.
- Do not buffer `recordingWriter` or change the write-through store. The body is already on the wire before Redis `Set`, so a store failure cannot become a 500. Leave that as a follow-up.
- Do not add another idempotent route.

## Impact Areas

- API / contract: yes (who may replay a change-password response)
- DB / migrations: no
- Auth / session: yes (principal id enters the Redis key)
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. Two principals, one operation id, and one header produce two Redis entries and two handler calls.
2. The same principal and header replays, with `Idempotency-Replayed: true`.
3. An idempotent route with no principal does not call the handler and does not write a key.
4. Change-password contract tests still pass, including guard-before-idempotency: an anonymous request does not prime the store.
5. The fingerprint deferral is a comment on the key builder, pointing at plan 1.8.

## Steps

- [ ] Add the user-id resolver at the composition site in the auth feature. Keep the Redis client option as it is.
- [ ] Put the user id in the key. Fail closed when the resolver returns no id.
- [ ] Comment the deferred body fingerprint.
- [ ] Unit-test two principals and the missing-principal case.
- [ ] Run `go test ./internal/platform/httpx ./internal/features/auth ./cmd/api` and `make verify`.

## Decision Log

- 2026-10-11: User id is enough. Backend also hashes a scope and the body. Scope here is already the operation id. The body hash stays the plan 1.8 follow-up.
- 2026-10-11: Write-through is not in this plan. Fixing it means holding the response until Redis accepts the record, which is a different design from the writer that exists today.

## Verification

```bash
go test ./internal/platform/httpx ./internal/features/auth ./cmd/api -count=1
make verify
```

## Completion Notes

## Follow-Ups

- [ ] Hold the response until the idempotency record is stored, or accept write-through in an ADR.
- [ ] Payload fingerprint when a second idempotent route needs it.
