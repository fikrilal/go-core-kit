# N5 — redis and password comments

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 05
- Related: `_WIP/readability/05-platform-redis-password.md`

## Objective

State the Redis open policy, the TTL rules, and the argon2 cost source. No dial, expiry, or hash-cost change.

## Constraints

- `Open` still parses the URL and does not dial.
- Expiry still attaches only when `ttl > 0`.
- `Verify` still reads `m`, `t`, and `p` from the encoded hash.
- Do not switch to `PTTL`, reject a PHC suffix, cap memory, or add an asynq client.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `Open` says it parses and does not dial, and that `SetLogger` replaces the process-wide driver logger.
2. `Set` and `SetNX` say only `ttl > 0` attaches an expiry. `TTL` says 0 means missing, no expiry, or under one second. `Expire` says a missing key returns nil.
3. `Verify` says the argon2 cost comes from the encoded hash, not from `Hasher.params`.

## Steps

- [x] Comment `Open`, `Set`, `SetNX`, `TTL`, `Expire`, and `Verify`.
- [x] `go test ./internal/platform/redis ./internal/platform/password`.

## Decision Log

- 2026-10-11: The probe facts stay comments. No caller passes a non-positive TTL, and no hash this process writes has a parameter suffix.

## Verification

```bash
go test ./internal/platform/redis ./internal/platform/password -count=1
```

Outcome (2026-10-11): both packages passed. No behavior change.

## Completion Notes

Comments only. Redis still does not dial on open. Argon2 cost still comes from the stored hash.

## Follow-Ups

None.
