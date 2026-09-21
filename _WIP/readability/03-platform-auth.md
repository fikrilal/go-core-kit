# Section note: `internal/platform/auth`

- Status: Draft, waiting for acceptance
- Date: 2026-10-11
- Scope: `auth.go`, `verifier.go`, `guard.go`, and their three test files
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: none in this pass
- Earlier notes stay unapplied: `01-platform-httpx.md`, `02-platform-primitives.md`

## Verdict

The split is already the right shape. `auth.go` owns keys and signing. `verifier.go` owns "is this access token good, and who is it". `guard.go` owns the HTTP decision. The feature does not parse JWTs. The clock is injected. Nothing here calls `time.Now()`.

`principalFromClaims` is the function to copy. It is a straight list of rejections, each one returns `ErrInvalidToken`, and the guard turns that single sentinel into 401. That is the mobile mapper habit: one translation at the edge, the checker stays free of HTTP.

The hard reading is `addKey`. It returns nothing, skips bad keys without a word, and stamps every surviving key as EdDSA. That stamp is also the real bug.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `auth.go` | 234 | Key ring, JWKS, EdDSA issuer |
| `auth_test.go` | 192 | Sign claims, ephemeral key, base64, prod requires keys |
| `verifier.go` | 178 | Signature and claim checks, `Principal` |
| `verifier_test.go` | 220 | Round trip and the rejection matrix |
| `guard.go` | 71 | Bearer header, 401, principal on the context |
| `guard_test.go` | 130 | Principal attached, missing and tampered headers |

Compared with `backend-core-kit` `libs/platform/auth/auth-keyring.service.ts` and `access-token-verifier.service.ts`. Claim checks match that verifier: 60 second skew on `iat` and `nbf` only, `exp <= now` is dead, `typ` must be `access`, empty issuer or audience means "do not check", `email_verified` is true only when the claim is boolean true.

## Hard to read

### 1. `addKey` swallows every failure

`auth.go` `addKey`. A key is dropped, with no error, when any of these happen:

- missing or blank `kid`
- `jwk.IsPrivateKey` fails
- public key cannot be derived
- setting `kid`, `use`, or `alg` fails

`NewKeyRing` then keeps going. Production fails only when zero private keys survived. A set with one broken key and one good key boots, and the broken key is never mentioned.

Backend does the same `continue` on a bad key. The behavior is family parity. The Go function is still the one a junior cannot trace, because the skips are bare `return`s inside a loop that also mutates `publicKeys`, `keysByKid`, and `signingKey`.

The policy that has to be visible, in this order:

1. First private key in the JWK set is the signer. Later private keys are published and can verify, and they do not sign.
2. A public-only key is published when some other key signs.
3. If nothing can sign, production returns the "no usable private keys" error. Any other environment throws the parsed set away and mints an ephemeral Ed25519 key. Supplied public keys are not kept. Backend does this too.
4. `KeysJSON` wins over `KeysJSONBase64` when both are non-empty after trim.

Do not fold ephemeral setup into `addKey` until `addKey` can report a skip. Ephemeral setup returns errors. `addKey` hides them. Merging those would make key generation fail silently.

### 2. The verifier parses claims into a map because `aud` has two shapes

`VerifyAccessToken` checks the signature, unmarshals the payload into `map[string]any`, then `principalFromClaims` type-asserts each field. The issuer already has `accessTokenPayload`, and a second struct would look cleaner. It would also be wrong. `aud` is a string or an array of strings. `TestVerifyAccessTokenAcceptsArrayAudienceAndSanitizesRoles` locks the array. The issuer struct can only emit a string.

Keep the map. One comment on `principalFromClaims` should say why it is not `accessTokenPayload`. `numberClaim`, `audienceMatches`, and `parseRoles` are the right small helpers. `audienceMatches` is the exhaustive switch. `parseRoles` drops non-strings, blanks, and duplicates. That is intentional and tested. A role of `"yes"` as a bare string, not an array, becomes no roles. The token still verifies. Privilege only goes down.

### 3. `Guard.Require` has a dead-looking 500 branch

`verifier.go` returns only `ErrInvalidToken`. The guard maps that to 401 `UNAUTHORIZED` with no detail. Any other error goes to `WriteError`, which becomes 500. Today the second branch cannot run.

Leave the branch. It is the seam between a bad token and a broken verifier. A comment of one line is enough: a client token never reaches it. Do not log the bearer value. Do not add a reason string to the 401 body. Backend also collapses every claim and signature failure into one invalid-token error. The matrix in `verifier_test.go` is where the reason stays visible.

## Correct Go that should stay

- Package split. Signing material, verification, and the HTTP guard are different jobs. OIDC stays in `internal/features/auth/infra/oidc.go`. A Google ID token is not a first-party access token.
- `ErrInvalidToken` is a sentinel. The guard uses `errors.Is`. Wrapping a bad token in `fmt.Errorf` would turn it into a 500. The bare sentinel is the contract inside this package.
- `NewVerifier` refuses an empty issuer or audience when `ProductionLike` is set. `NewKeyRing` refuses a missing key set in that same case. Both match backend. The bool comes from `config.ProductionLike`, which the primitives note already covered. The checks stay here, not in `config.Validate`.
- Empty issuer and empty audience skip those claim checks. A dev ephemeral key can omit them. `SignAccessToken` uses `omitempty`, and `TestSignAccessTokenOmitsEmptyIssuerAndAudience` locks that. Production cannot construct a verifier with both empty.
- Clock skew matches backend exactly. `iat` and `nbf` may sit up to 60 seconds in the future. `exp <= now` is already expired, with no skew. Do not "fix" expiry to be generous.
- `email_verified` must be JSON `true`. The string `"yes"` becomes false, not an error. Tested. Same rule as backend `=== true`.
- Algorithm is checked on signature 0 before `jws.Verify`, and `Verify` is called with `jwa.EdDSA()` only. `alg: none` and `alg: RS256` never reach the key. The rejection test named "alg mismatch" locks this.
- `bearerToken` accepts any case of `Bearer`, requires whitespace, and trims the token. `Basic`, a bare `Bearer`, and a missing header are 401. Tested through the real router, so the problem has a `traceId`.
- Context key is an unexported struct. Callers use `WithPrincipal` and `PrincipalFrom`. A string key would be the collision.
- `PublicJWKS` returns `slices.Clone`. Callers cannot append into the ring. Published keys are the public half from `key.PublicKey()`, not the signing key. Plan 3.4 recorded a live JWKS check that `d` is absent. The unit tests do not repeat that assertion.
- `jti` is a new UUID at sign time. The verifier checks non-empty and length `<= 128`. It does not store `jti`. Access tokens are not replay-cached. Backend is the same.
- Tests live in `auth_test`. They only use the public API, except the raw signer in `verifier_test.go`, which has to build bad tokens the issuer refuses to mint. That raw signer should stay.
- `fixedMoment` is shared by the three test files. One clock instant, so `iat` and `exp` assertions are obvious.

## Real bugs

### B1. A non-Ed25519 private key boots, and the JWKS lies

Probed 2026-10-11 with a 2048-bit RSA private JWK, `kid` `rsa-kid`, `ProductionLike: true`.

`NewKeyRing` returned a ring. `Kid()` was `rsa-kid`. The published document was `kty: RSA` with `alg: EdDSA` and `use: sig`. No private field. The first `SignAccessToken` then failed:

```text
auth: sign access token: jws.Sign: algorithm "EdDSA" is not compatible with key type *jwk.rsaPrivateKey
```

Startup does not fail. `/.well-known/jwks.json` would advertise a key that cannot verify an EdDSA token and is not an EdDSA key. The first login or register fails inside token issuance, after the user and session work, unless the caller notices the sign error.

`addKey` writes `jwa.EdDSA()` onto every key and never reads `kty`. Backend infers `OKP` as EdDSA and `RSA` as RS256, and signs with that algorithm. This probe is not that feature. It is fail-open loading plus a false `alg`.

When accepted as a bug: `NewKeyRing` should reject a private key that cannot sign EdDSA, before the process serves traffic. The JWKS must not label another key type as EdDSA. A test with an RSA JWK belongs next to `TestKeyRingRequiresKeysInProductionLike`.

### B2. RS256 is advertised and not implemented

`docs/core/project-stack.md` says the auth stack is "EdDSA default, RS256 supported". Backend verifies both. A compact RS256 access token is `ErrInvalidToken` here, and `TestVerifyAccessTokenRejections` calls that case "alg mismatch" on purpose.

Plan 3.4 shipped EdDSA only. The code and the tests agree with the plan. The stack doc and the backend verifier do not. A client holding a backend RS256 access token cannot call this process. A key set that is legal on backend (B1) does not sign here.

Do not add RS256 inside a readability edit. Either correct the stack sentence to "EdDSA only", or schedule RS256 as its own change with a verifier switch and a keyring that keeps the key's real algorithm. B1 should be fixed even if RS256 stays unsupported. A rejected key must fail `NewKeyRing`, not the first request.

### B3. Whitespace counts as a configured issuer

`NewVerifier` treats `""` as missing and `" "` as present. Production with `AUTH_ISSUER=" "` boots. Every real token then fails `iss`, because the claim is compared to a space. Backend uses a trim-and-reject helper, so a blank value is the same as unset and production refuses to start.

`NewKeyRing` already trims the key JSON. The issuer and audience strings should get the same trim before the production check and before they are stored. Small, and it changes which process starts.

## Design question

What still sits in the feature, and what must not move into this package?

The guard is allowed to build `httpx.ProblemError`. It is the HTTP edge for this package. `ErrInvalidToken` in the verifier and `unauthorized()` in the guard are the pair. Do not push that 401 into `httpx`, and do not push it down into `features/auth/app`.

Refresh, password, and OIDC failures are not access-token failures. They stay in the feature, in one mapper, which is the auth-feature note. This package should not grow `AUTH_REFRESH_TOKEN_REUSED` or `AUTH_OIDC_TOKEN_INVALID`.

`issuerAdapter` in `internal/features/auth/auth.go` is the right thickness. It copies `UserID`, `SessionID`, `EmailVerified`, and one role into `AccessClaims` and signs. It does not touch JWK. Leave it.

`NewIssuer` does not reject `ttl <= 0`. `config.Validate` already requires the access TTL to be at least 1 second, and that is the only caller. A zero TTL mints a token that is already expired under `exp <= now`. Not a live bug. Do not add a second policy check until a second caller exists.

## Function notes

### `auth.go`

- `JWKS`, `KeyRing`, `KeyRingOptions`, `NewKeyRing`. Finding 1 and bug B1. Base64 uses `StdEncoding`, so the text must be padded. Backend's decoder also accepts unpadded base64. A normal `base64` of the JSON file is padded. Constraint, not a current failure.
- `addKey`. Finding 1. Duplicate `kid` means the map keeps the last key and the JWKS slice keeps both. Backend's array and map do the same. Leave it.
- `ephemeralKeyRing`. Clean. Errors are returned. The three `Set` calls repeat `addKey` on purpose until finding 1 is fixed.
- `SigningKey`, `Kid`, `PublicJWKS`, `PublicKeyForKid`. Clean.
- `Issuer`, `NewIssuer`, `AccessClaims`, `accessTokenPayload`. Clean. Field names match the claim list in plan 3.2.
- `SignAccessToken`. Clean sequence: marshal, set `typ` and `kid`, sign EdDSA. Header `typ` is `JWT`. Payload `typ` is `access`. Those are different claims. The tests check both. Easy to "simplify" into one and break the contract. Do not.

### `auth_test.go`

Covers the happy claims, omitted `iss` and `aud`, prod without keys, ephemeral sign, and base64. Does not cover a public-only set, two private keys, or bug B1. `testKeysJSON` builds a key through `jwk`. `verifier_test.go` builds a raw OKP map. Two fixtures is fine. The raw map is what makes the rejection tests honest.

### `verifier.go`

- Constants. `maxAccessTokenLength` is 16384. `maxJtiLength` is 128. `jwtClockSkewSeconds` is 60. Same numbers as backend.
- `Principal`, `ErrInvalidToken`, `Verifier`, `NewVerifier`. Bug B3 on the empty check.
- `VerifyAccessToken`. Clean. Bad JSON after a valid signature is `ErrInvalidToken`, not a 500.
- `verifiedPayload`. Readable top to bottom. Only signature 0 is inspected. Compact JWTs have one signature. A JWS JSON with several signatures is outside what this kit issues. Residual, not a second parser.
- `principalFromClaims`. The model function. `nowSeconds` is a `float64` because `encoding/json` decodes claim numbers into `float64`. Unix seconds are exact in that range.
- `numberClaim`, `audienceMatches`, `parseRoles`. Clean.

### `verifier_test.go`

The rejection list is the right test. It is long because each token is a different decision, not because of noise. `withClaim` and `withoutClaim` mutate the map they are given. Every call site passes a new `baseClaims()`, and `signRaw` runs before the next case, so the tokens are not corrupted. Reusing one map across two `withClaim` calls would silently change an already-built case's source. Worth knowing before editing the table.

`otherRing` is built and then discarded with `_ = otherRing`. The other key is what the test needs. The ring is leftover. Noise, not a failure.

No test that a token expiring in the same second is invalid. The `<=` is the same line as the "expired" case, which moves the clock 16 minutes. Low.

### `guard.go`

- `WithPrincipal`, `PrincipalFrom`. Clean.
- `Guard`, `NewGuard`, `Require`. Finding 3. Nil verifier panics on the first request. `auth.Register` always passes the verifier it just built.
- `unauthorized`, `bearerToken`. Clean.

### `guard_test.go`

The protected handler JSON-encodes the `Principal` struct, so the test decodes `UserID`, not `userId`. That encoding is the test double. Production handlers do not write the principal as JSON. The 401 cases check the code and a non-empty `traceId`. They do not check that the body is `application/problem+json`. The router test in `httpx` already covers the content type. Enough.

## What acceptance authorizes

Taste, no token-format change:

1. Comment the four key-selection rules on `NewKeyRing` / `addKey`. Keep the silent skip until a caller needs the skip reason.
2. Comment why `principalFromClaims` uses a map.
3. Comment why the guard's non-`ErrInvalidToken` branch is a 500.

Bugs, separate yes:

- B1. Reject a private key that cannot sign EdDSA. Do not publish `alg: EdDSA` on it.
- B2. Either fix `docs/core/project-stack.md` to say EdDSA only, or plan RS256 as its own change. Not both in a comment-only pass. B1 does not wait on that choice.
- B3. Trim issuer and audience, then apply the production-empty check.

Not in this acceptance:

- Implementing RS256 verify or sign.
- Moving refresh or OIDC errors into this package.
- Unifying `ephemeralKeyRing` with `addKey`.
- Logging why a token was rejected.
- Applying the `httpx` or primitives notes.

## Verification

No production edit. `make verify` was not run.

Probe, deleted afterward (`internal/platform/auth/probe_audit_test.go` is not in the tree):

```bash
go test ./internal/platform/auth -run TestAuditKeyProbe -count=1
```

Result: pass. The load, the JWKS `alg`, and the sign error are quoted in B1. The probe is not coverage.
