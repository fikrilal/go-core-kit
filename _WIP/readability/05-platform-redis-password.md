# Section note: `internal/platform/redis` and `internal/platform/password`

- Status: Comments shipped in N5.
- Date: 2026-10-11
- Scope: `internal/platform/redis` and `internal/platform/password`, including their tests
- Out of scope: the login limiter and the dummy-hash login path. Those are auth-feature policy. This note only records how they call these two packages.
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: comments applied 2026-10-11. See `docs/exec-plans/completed/n5-redis-password-comments.md`.
- Earlier notes 01 through 04 are applied.

## Verdict

Both packages are already the shared mechanic. Features do not import `github.com/redis/go-redis` (depguard) and do not call `argon2` themselves. Redis is a small client plus `Key`. Password is PHC argon2id plus the reference-kit cost. The login sequence that uses them — block check, verify, count failure, clear on success — stays in the auth feature, which is where that policy belongs.

Nothing here is a wrong row or a wrong HTTP status. The hard lines are comments that do not match the driver, and `Verify` ignoring the hasher's own parameters.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `internal/platform/redis/redis.go` | 130 | Open, ping, get/set/incr, key join |
| `internal/platform/redis/redis_test.go` | 71 | Bad URL, key join, ping, get/set/setnx/del |
| `internal/platform/password/password.go` | 123 | Argon2id PHC hash and verify |
| `internal/platform/password/password_test.go` | 78 | Reference hash, round trip, malformed input |

Callers, read far enough to see the contract they need:

- `cmd/api/main.go` uses `redis.Open`. Tests and `cmd/api/coverage_test.go` use `MustOpen`, including `redis://127.0.0.1:1/0`, which parses and does not dial.
- `httpx` idempotency uses `Get`, `Set`, `SetNX`, `Del`, and `Key("idempotency", scope, header)`.
- `internal/features/auth/infra/limiter.go` uses `Get`, `TTL`, `Incr`, `Expire`, `Set`, `Del`, and `Key("auth", "login", ...)`.
- `internal/features/auth/auth.go` calls `password.New()` and `Hash` once at boot for the dummy hash. `app.Service` calls `Hash` and `Verify`. Min length stays in `checkPasswordPolicy`.

Compared with backend-core-kit `argon2.password-hasher.ts` (library defaults, verify the stored string) and `redis-login-rate-limiter.ts` (`ttl > 0`, otherwise the configured block). ADR 0006 is the Redis role list. Plan 1.6 is the client. Plan 1.10 silenced the driver logger. Plan 3.2 fixed the PHC length bound.

There is no queue client in this package. Plan 1.6 says asynq will parse `REDIS_URL` itself. Do not add that here.

## Hard to read

### 1. `Open` says it connects

```go
// Open connects to Redis using the configured URL.
```

`goredis.ParseURL` plus `NewClient` does not dial. `MustOpen("redis://127.0.0.1:1/0")` in `cmd/api/coverage_test.go` returns a client. The first real command fails later. That is the same shape as `sql.Open`, and plan 1.6 says boot must not ping. The comment is the part that lies.

`Open` also calls `goredis.SetLogger(quietLogger{})`. That replaces the logger for every go-redis client in the process, not for this client. The comment on `quietLogger` explains why the logs are discarded. It does not say the call is process-wide. `formatMs` warnings for a sub-millisecond TTL die in that logger. No current caller passes a sub-millisecond TTL.

Say both facts on `Open`: it parses, and it installs one quiet logger for the process.

### 2. `TTL` returning 0 is three states, and a non-positive `Set` does not expire

`TTL`'s comment says missing or persistent keys report 0. Probed 2026-10-11 against miniredis:

```text
missing TTL=0s
Set(..., 0) stores the value, TTL=0s
Set(..., 50ms) stores the value, TTL=0s
Set(..., -5) stores the value, TTL=0s
Set(..., -1) stores the value
Expire on a missing key returns nil
```

Redis `TTL` is whole seconds. A key with less than one second left is 0 while `Get` still finds it. go-redis also maps the missing key (`-2`) and the persistent key (`-1`) onto negative durations, and this wrapper turns every negative into 0. So 0 means missing, no expiry, or a remainder under one second.

`Set` and `SetNX` document, in the driver, that expiration 0 means no expiry. A negative duration is not rejected. `Set` persists it. `SetNX` only special-cases 0 and the driver's keep-ttl sentinel (`-1`); other negatives go through the expiry argument. No caller passes those values. Idempotency uses 30 seconds and 24 hours. The limiter uses the configured window and block, and `config.Validate` already requires those integers to be at least 1.

`Expire` drops the driver's bool. Redis returns false when the key is already gone, and this wrapper returns nil. The limiter treats a nil error as "the window was set". It only expires a key it just incremented, so the miss is a race, and the backend limiter also ignores that flag. The comment should say a missing key is not an error.

The login limiter already turns `ttl <= 0` into the full block duration. That matches backend `getRetryAfterSeconds`. Do not switch this client to `PTTL`. A finer TTL would change `Retry-After` away from the reference kit.

### 3. `Verify` does not use the hasher's parameters

`Hash` reads `h.params`. `Verify` decodes `m`, `t`, and `p` from the stored string and passes those to `argon2.IDKey`. `NewWithParameters` changes only future hashes. Old hashes keep the cost written into them. That is how PHC works, and how `@node-rs/argon2` `verify` works. A reader who changes `DefaultParameters` and expects existing passwords to be checked at the new cost will misread the login path.

`DefaultParameters` is the reference default: 19456 KiB, 2 iterations, parallelism 1, 16-byte salt, 32-byte key. The test locks the prefix `$argon2id$v=19$m=19456,t=2,p=1$` and a real hash from that library.

`NewWithParameters` has no caller. A zero `Iterations` panics inside the library (`argon2: number of rounds too small`), probed 2026-10-11. Do not add a second validator until something calls it. One comment on `Verify` is the fix: the cost is the hash's cost.

`Hash` emits unpadded base64 (`RawStdEncoding`). A padded segment fails decode (`password: malformed salt`, probed). The round-trip test already rejects `=`. Leave the encoding.

## Correct Go that should stay

- The driver stays behind `Client`. There is no `SQL()`-style escape hatch, and there should not be one. A new command is a new method. Features keep the sequences: idempotency in `httpx`, login limits in `infra.LoginLimiter`.
- `Get` returns `("", false, nil)` for a missing key and `("", true, nil)` for a stored empty string. Probed. Callers use the bool. `goredis.Nil` is the only error turned into "missing". Every other error is wrapped. That is what makes idempotency fail closed.
- `SetNX`'s bool means "this call stored the value". Idempotency treats false as "someone else holds the lock". Keep that meaning in the name at the call site when the httpx note is applied. Do not rename it here.
- `Key` is `strings.Join(parts, ":")`. `Key()` is `""`. `Key("auth", "", "login")` is `auth::login`. Callers pass non-empty pieces. Do not add a validator.
- `Del()` with no keys returns `del redis key: ERR wrong number of arguments for 'del' command`. `RecordSuccess` always passes the email counter. Do not special-case the empty call.
- `Incr`, `Expire`, and `TTL` have no test in `redis_test.go`. `limiter_test.go` is the test: threshold, `Retry-After`, and clear-on-success. Leave them there.
- `Ping` wraps the error so `health.Check{Verify: client.Ping}` stays a method value. `MustOpen` panics on a bad URL. `cmd/api` uses `Open`. Same split as `config.MustLoad`.
- `subtle.ConstantTimeCompare` is the verify check. Do not replace it with `==`.
- `decode` rejects a non-`argon2id` id, a version other than `argon2.Version`, a zero `m`, `t`, or `p`, a bad base64 segment, and a salt or key outside `1 .. 1<<20`. The cap is the plan 3.2 gosec bound. A 1-byte key does not panic: `Verify` returns false. Probed.
- `fmt.Sscanf` on `m=%d,t=%d,p=%d` does not require the segment to end. Probed: `p=1junk` on the reference hash still verifies. Hashes this process writes have no suffix. Rejecting the suffix would be a new parser, not a readability edit. `p=256` fails with an overflow error because `Parallelism` is a `uint8`. That one already fails closed.
- `Verify` does not cap `MemoryKiB`. A replaced `password_hash` can ask argon2 for a huge allocation. The reference `verify` trusts the stored parameters the same way. Our `Hash` only writes 19456. Do not add a ceiling in this pass.
- Password policy and the dummy hash stay out of this package. `dummy-password-for-timing` is hashed once in `auth.Register`. Login picks the stored hash or that dummy, then calls `Verify`. Unknown email and wrong password both pay the argon2 cost. That choice is the feature.

## Real bugs

None in these two packages.

Checked and not bugs: non-positive TTL persistence (no caller), `Expire` on a missing key (race, same as the backend), `TTL` of 0 for a sub-second key (same `Retry-After` fallback as the backend), `p=1junk` still verifying (no hash we write), zero-parameter `Hash` panicking (no caller), uncapped `m` on a tampered row (same as the reference verifier).

## Design question

What is still in the feature, and what must stay here?

The client and the PHC codec stay here. The product rules stay in the feature:

- which Redis keys exist (`auth:login:email:<hash>:failures` and the ip twin)
- when a counter becomes a block
- when success deletes counters
- minimum password length
- which hash is verified for a missing user

Do not grow `platform/redis` into a rate limiter. Do not grow `platform/password` into a policy checker. The next feature that needs a counter should call `Incr` and `Expire`, not a second helper with the login numbers baked in.

## Function notes

### `redis.go`

- `Open`, `MustOpen`. Finding 1.
- `Ping`, `Close`. Clean.
- `Get`. Existence is the bool. Clean.
- `Set`, `SetNX`. Finding 2. The bool on `SetNX` is "stored".
- `Del`. Empty argument list errors. Clean for real callers.
- `Incr`. Clean. A non-integer value at that key is a driver error. The limiter only increments its own counters.
- `Expire`, `TTL`. Finding 2.
- `Key`. Clean.

### `password.go`

- `Parameters`, `DefaultParameters`, `New`. Clean.
- `NewWithParameters`. Finding 3. Unused.
- `Hash`. Uses `h.params`, random salt, unpadded PHC. Clean once finding 3 is commented.
- `Verify`. Finding 3. Constant-time compare. Decode errors stay errors, so a corrupt stored hash is not reported as a wrong password. Login turns that error into a wrapped failure. That mapping is the feature note.
- `decode`. Rejects the cases listed above. The `v=` check is dense and correct: missing prefix, bad integer, and wrong version are one condition.

### Tests

`redis_test.go` covers a bad URL, colon join, ping success, a simulated outage, close, missing get, set, both `SetNX` results, and delete. It does not cover `Incr`, `Expire`, `TTL`, or a non-positive TTL.

`password_test.go` covers the reference hash, a wrong password, a random salt, the PHC prefix, unpadded segments, and a set of malformed strings. It does not cover `NewWithParameters`.

## What acceptance authorizes

Taste, comments only:

1. On `Open`: it parses the URL and does not dial, and `SetLogger` replaces the process-wide driver logger.
2. On `Set` and `SetNX`: only `ttl > 0` attaches an expiry. On `TTL`: 0 means missing, no expiry, or under one second. On `Expire`: a missing key returns nil.
3. On `Verify`: argon2 cost comes from the encoded hash, not from `Hasher.params`.

Bugs: none. No separate yes.

Not in this acceptance:

- `PTTL`, or changing the limiter's `Retry-After` fallback.
- Rejecting a PHC parameter suffix.
- Capping `MemoryKiB`.
- Validating `NewWithParameters`.
- Moving `LoginLimiter` or the dummy hash into these packages.
- An asynq client.
- Applying the earlier notes.

## Verification

Comments applied 2026-10-11. See `docs/exec-plans/completed/n5-redis-password-comments.md`.

Probes, deleted afterward (not in the tree):

```bash
go test ./internal/platform/redis ./internal/platform/password -run 'TestAudit' -count=1 -v
```

Result: pass. The TTL, `Set`, `Expire`, empty `Del`, trailing `p=1junk`, `p=256`, short key, padded salt, and zero-parameter panic lines are quoted above. The probes are not coverage.
