# Section note: `internal/features/auth`

- Status: Draft, waiting for acceptance
- Date: 2026-10-11
- Scope: the auth feature. `app` against the thin use case, `infra` against repository orchestration, `domain` against an exhaustive outcome switch. `auth.go` is the HTTP edge and the wiring.
- Out of scope: `internal/platform/auth`, `password`, `redis`, `httpx`, and the SQL text. Those notes already stand. This note only records how the feature calls them.
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: none in this pass
- Earlier notes stay unapplied

## Verdict

This is already the feature shape the later slices should copy. A handler decodes, calls one service method, and writes. The service states the policy. The repository runs the SQL sequence inside `InTx` and returns a sentinel or a kind. Domain types do not know HTTP or the database.

`Refresh` is the function to copy. It names every rotation outcome, and the default rejects a kind the switch does not know. `rotateExisting` is the repository function to copy: one transaction, the reuse rule in order, no HTTP.

The miss is the problem table. Most outcomes already have a named helper at the bottom of `service.go`. Three decisions still build `*httpx.ProblemError` inline, and the helpers live in the same file as the policy, so the use case scrolls through HTTP. Mobile keeps that table beside the use case. It does not go into `httpx`, and it does not go into `domain`.

No live bug. The odd branches match backend-core-kit, including the ones that look wrong on a first read.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `auth.go` | 315 | Wiring, handlers, wire DTOs. `//go:build !pg` |
| `domain/domain.go` | 181 | Types, email, refresh token, user-agent |
| `ports/ports.go` | 100 | Repository and the other ports |
| `app/service.go` | 571 | Register, login, refresh, logout, change password, OIDC |
| `app/service_test.go` | 839 | Fakes and the outcome table |
| `infra/repository.go` | 500 | sqlc sequences. `//go:build !pg` |
| `infra/limiter.go` | 118 | Login counter and block |
| `infra/limiter_test.go` | 98 | Threshold, clear, immediate block |
| `infra/oidc.go` | 194 | Google ID token verifier |
| `infra/oidc_test.go` | 196 | Accept, reject, JWKS outage |

Compared with backend-core-kit `password-auth.service.ts`, `oidc-auth.service.ts`, `google-oidc-id-token-verifier.ts`, and `normalizeUserAgent`. Contract tests under `cmd/api` were not re-read line by line. The unit tests name the outcomes this note relies on.

`auth.go` and `repository.go` are excluded from a Postgres build. There is still no `//go:build pg` repository. Same gap as `platform/db`. Not a defect in the SQLite feature.

## Hard to read

### 1. The use case still builds HTTP in three places

`app` imports `httpx` because the service returns `*httpx.ProblemError`. The handlers never choose a status. That split is right: `WriteError` already understands that type, and a second domain error alphabet would only give the handler a switch.

The table is the helpers at the bottom of `service.go`: invalid credentials, the four refresh failures, suspended, unauthorized, password-not-set, current-password-invalid, and the four OIDC failures. `Refresh`, `ChangePassword`, and `ExchangeOidc` call those names. A reader sees the outcome, then the HTTP, in one place.

Three decisions do not:

- `Register`, on `domain.ErrEmailExists`, builds the 409 inline.
- `ChangePassword`, when the two passwords match, builds the 400 inline.
- `checkPasswordPolicy` builds the 400 inline. It has to stay a method: the field is `password` on register and `newPassword` on change, and the message includes the configured minimum. That is why it is not a `validate` tag.

Move the helpers to `app/problems.go`. Point the three inline constructions at named helpers in that file. The service methods then read as policy. Do not move the table into `httpx` or `domain`. Domain stays free of HTTP.

### 2. `ExchangeOidc` does not reject an unknown kind

`Refresh` and `ChangePassword` end the switch with an error that names the unexpected value. `ExchangeOidc` does this:

```go
switch verification.Kind {
case domain.OidcVerifyNotConfigured:
    return Result{}, oidcNotConfiguredError()
case domain.OidcVerifyInvalid:
    return Result{}, oidcTokenInvalidError()
case domain.OidcVerifyVerified:
}
```

The verified arm is empty. Any other string falls through. A zero identity has `EmailVerified == false`, so the unknown kind becomes 400 `AUTH_OIDC_EMAIL_NOT_VERIFIED`. Nothing returns that kind today. The empty arm is still the line a junior misreads as "do nothing".

Give `OidcVerifyVerified` an empty body that says it continues, and add the same default the refresh switch has. No current response changes.

### 3. `Refresh` decides twice

`Refresh` loads the token, rejects deleted, suspended, revoked, expired, and reused, and on reuse revokes the family. It then signs an access token and calls `RotateRefreshToken`, which checks the same facts again inside the transaction and may revoke the family again.

The comment above the sign call is the important half: the access token is minted before rotation so a failed rotate does not consume the caller's refresh token. The other half is missing. The first list is the fast path. `rotateExisting` is the decision that wins a race. Leaving both is correct. Say that next to the first list so it does not look like a copy-paste.

`issueSession` does the opposite order: it stores the session, then signs. A sign failure after a successful insert leaves a session the client never received. Refresh cannot do that, because the old refresh token would already be consumed. Login, register, and OIDC can. Do not reorder `issueSession` to match refresh.

## Correct Go that should stay

- Handlers stay dumb. `register` and `login` share `password`. OIDC, refresh, logout, and change-password repeat decode, call, `WriteError`. That repetition is the httpx habit. Do not hide it.
- `respondAuth` loads `MeView` after the service returns. `Result.User` has no profile and no auth methods. Register's returned user is also constructed in the repository (`RoleUser`, `StatusActive`, unverified), not read back. The access token is signed from that value. The JSON body is the second read, which left-joins `user_profiles`. A password user with no profile still returns. Empty profile strings become JSON null through `nullableString`.
- `clientIP` is `RemoteAddr` only. A spoofed `X-Forwarded-For` does not pick the rate-limit bucket. There is no trusted-proxy setting. Leave it.
- `changePassword` checks the principal again after `guard.Require`. The guard is the real check. The handler branch is what a direct call hits. The route is `guard.Require(router.Idempotent(...))`, guard outside the replay store.
- `issuerAdapter` is the whole bridge from `domain.User` to platform access claims. One role string becomes `[]string{user.Role}`.
- JWKS uses `httpx.Document`. It stays unenveloped.
- Login matches the reference service. Unknown user verifies the dummy hash. Wrong password, unknown user, and `DELETED` record a failure and return `AUTH_INVALID_CREDENTIALS`. `SUSPENDED` does not record a failure and does not clear the counter. The `!ok || !verified || deleted` line is dense because the reference treats those three as one outcome. A comment belongs on it when the helpers move. Do not split it into a status switch that changes when the counter increments.
- `SessionTTL` is `AUTH_REFRESH_TOKEN_TTL_SECONDS`. `issueSession` writes that instant onto both the session and the refresh token. The access-token TTL stays on the platform issuer. One comment on that assignment is enough.
- Repository outcomes use two shapes on purpose. `ErrEmailExists` and `ErrExternalIdentityExists` are sentinels, wrapped with `%w`, so `errors.Is` survives `InTx`. Rotation and password-change results are kinds returned next to a nil error, because they are successful reads with a named ending. Do not collapse those into one style.
- `CreateUserWithPassword` maps only the user-row unique violation to `ErrEmailExists`. The credential insert is in the same transaction. A failure there rolls the user back.
- `RotateRefreshToken` and `ChangePasswordAndRevokeOtherSessions` set the kind inside the transaction and return nil to commit. A non-nil error discards the kind. `updated != 1` on `MarkRefreshTokenRotated` is reuse, and the family is revoked in that same transaction.
- `RevokeSessionByRefreshToken` still finds an already-revoked token and revokes again. Logout stays 204 on repeat. An unknown hash is 401.
- `orderAuthMethods` forces `PASSWORD` then `GOOGLE`. `GetAuthMethods` is a `UNION` and does not order. A user with both methods is not reachable through the current routes (an email collision returns link-required and does not attach Google). The sort is still the contract for that row. No unit test locks the order. Leave the function.
- OIDC email collision, including the create race, is `AUTH_OIDC_LINK_REQUIRED`. Only `ErrExternalIdentityExists` refetches the winner. Backend `oidc-auth.service.ts` and its race test do the same. Do not refetch on the email conflict.
- Deleted OIDC users return `invalidCredentials`, not an OIDC code. The reference does that because its deleted code is outside the declared set. The unit test locks it.
- `GoogleIDTokenVerifier` returns a kind and a nil error for every rejection, including a JWKS outage (`TestGoogleVerifierFailsClosedWhenJWKSUnavailable`). The backend `jwtVerify` catch does the same. `email_verified` accepts a boolean or the strings `true` and `false`, matching `parseEmailVerified` in the reference verifier. The access-token verifier does not accept the string. The difference is the claim source. Four `nolint:nilerr` comments are the same rule repeated. One comment on `VerifyIDToken` should state it. Keep the nolints if the linter still wants them.
- The JWKS cache is 15 minutes, and the mutex is held across the fetch so two refreshes do not run. A verification waits. There is no refetch when the `kid` is missing. Google publishes the next cert before it removes the current one, so the window is not a live outage. Do not delete the cache, and do not switch to `PTTL`-style refetch in this pass.
- `NewGoogleIDTokenVerifier` replaces an all-whitespace URL with Google's endpoint. A URL that is not all whitespace is stored unchanged. The production config leaves `AUTH_OIDC_GOOGLE_JWKS_URL` empty and hits the default. Do not trim further here.
- `parseClientIDs` trims and drops empty pieces. That is the reference parser.
- Password minimum is rune count. JavaScript `password.length` is UTF-16 code units, so a password made only of non-BMP characters can pass one kit and fail the other. Rune count is the policy. Do not switch to UTF-16.
- `NormalizeUserAgent` caps at 512 bytes. The reference caps at 512 UTF-16 code units. The value is session metadata, not a response field. A cut can split a rune. Leave it.
- `ActiveSessionKey` and `NormalizeUserAgent` return a bool the service ignores. `""` is what `CreateSession` and `NULLIF` already treat as absent. Leave the signatures.
- The limiter stays in the feature. Thresholds and key names are login policy. `platform/redis` stays the client.
- `app/service_test.go` fakes the ports. That is why the service can be read without a database. Repository behavior is covered by the contract tests and the schema tests, not by a second fake. Do not add a sqlc mock.

## Real bugs

None.

Checked against the reference and not bugs: deleted-user masking, suspended login not clearing the counter, OIDC email-race mapped to link-required, JWKS failure mapped to invalid, string `email_verified`, no clock skew on Google `exp` (`exp == now` is dead, jose's default tolerance is 0), dummy-hash verification before the user is known, and plaintext equality for "new password must be different".

## Design question

What is still in the feature, and what must stay here?

Platform already hashes, signs, verifies access tokens, guards the route, stores idempotency, and runs SQL. The feature owns the product rules: who is allowed in, what consumes a refresh token, which sessions die on password change, and which outcome becomes which problem.

The handler does not become the mapper. The repository does not return `*httpx.ProblemError`. The service stays the policy and calls the problem helpers. That is the Go version of the mobile use case calling a repository and leaving the failure table beside it.

Do not split `Service` into one type per use case. The methods are already the use cases. Do not merge `app.ChangePasswordInput` with `ports.ChangePasswordInput`. One carries the plaintext passwords. The other carries hashes and the compare-and-set.

## Function notes

### `auth.go`

`Register` is a straight composition: key ring, hasher, dummy hash, service, verifier, guard, routes. The build tag needs a one-line package comment that this file is SQLite until a pg repository exists.

`authResult` and `refreshResult` are the wire boundary. Refresh omits auth methods. `nullableString` turns `""` into a JSON null. Leave both.

### `domain.go`

Types, two sentinels, the kind constants, and the token helpers. `NewRefreshToken` is 32 random bytes, raw base64url. `HashRefreshToken` is SHA-256, also raw base64url. No HTTP. Leave it.

### `service.go`

Finding 1, 2, and 3. `issueSession` is the shared register, login, and OIDC ending. `MeView` is a pass-through plus a not-found error that becomes a 500, which is right: the handler only calls it after a user was just loaded.

`Login` wraps the limiter error with `%w`. A block is already a `ProblemError`, and `WriteError` uses `errors.As`, so the 429 survives the wrap. A Redis failure stays a 500. Leave the wrap.

### `repository.go`

`rotateExisting` and `ChangePasswordAndRevokeOtherSessions` are the sequences. The repeated user-struct literals stay. A mapper type would hide which column became `EmailVerified`.

`GetMeView` then `GetAuthMethods` are two queries without a transaction. A method cannot change between them in any flow this process has. Leave them.

Build-tag comment, same as `auth.go`.

### `oidc.go`

Finding 2's source of kinds, plus the comments in the correct-Go list. `optionalStringClaim` turns a missing name into `""`, and the SQL `NULLIF` stores NULL. Same end state as the reference, which omits the empty field before insert.

### `limiter.go`

Already the readable policy. No change from this note.

### Tests

Service tests lock policy, dummy hash, reuse containment, mint-before-rotate, kind maps, OIDC races, and deleted masking. They do not lock auth-method order. Limiter tests lock the threshold. OIDC tests lock the verifier, including the string forms and the JWKS outage.

## What acceptance authorizes

Taste:

1. Move the problem helpers to `app/problems.go`. Replace the three inline `ProblemError` values with named helpers. `checkPasswordPolicy` stays on `Service` and calls one of those helpers.
2. Make the `ExchangeOidc` kind switch exhaustive, with a default that reports the unexpected kind. The verified arm stays a fall-through into the identity checks.
3. Comments, and nothing else:
   - `auth.go` and `repository.go`: SQLite build until a pg repository exists.
   - `Refresh`: the first checks and `rotateExisting` are the same outcomes; the transaction wins a race.
   - `Login`: deleted shares the invalid-credentials path on purpose.
   - `ExchangeOidc`: deleted OIDC users use that same password error on purpose.
   - `issueSession`: `SessionTTL` is both the session expiry and the refresh-token expiry.
   - `respondAuth`: the second read is the Me profile and auth methods.
   - `CreateUserWithPassword`: the returned user is constructed; the token uses it; the HTTP body uses `MeView`.
   - `VerifyIDToken`: a failure, including JWKS, is the invalid kind; string `email_verified` is intentional and is not how access tokens work.
   - `keySet`: the mutex is held across the fetch.
   - `orderAuthMethods`: `UNION` does not order rows.

Bugs: none. No separate yes.

Not in this acceptance:

- Reordering `issueSession` to sign before insert.
- Deleting the `Refresh` pre-check.
- Refetching the user on an OIDC email conflict.
- A repository user-mapper, a per-use-case type, or merging the two `ChangePasswordInput` structs.
- UTF-16 password length, a rune-safe user-agent cut, trusted proxy headers, or a JWKS refetch on an unknown `kid`.
- A pg repository, an OIDC connect route, or a Me route.
- Applying the earlier notes.

## Verification

No production edit. `make verify` was not run. No probe. The outcome claims above are the existing unit tests and the reference services, not a new run.
