# N3 — auth comments

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 03
- Related: `_WIP/readability/03-platform-auth.md`

## Objective

Make the key-selection rules, the claim map, and the guard's 500 branch readable. No token, JWKS, or startup behavior changes.

## Constraints

- `addKey` still returns on a bad key and does not report why.
- Do not reject a non-EdDSA private key. Do not change `alg` stamping. That is bug B1.
- Do not trim issuer or audience. That is bug B3.
- Do not edit `docs/core/project-stack.md` and do not add RS256. That is bug B2.
- Do not merge `ephemeralKeyRing` into `addKey`.
- Do not log a rejected token or add a reason to the 401 body.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `NewKeyRing` and `addKey` state the four rules: the first private key signs, later private keys are published and do not sign, a public-only key is published when another key signs, and a set with no signer errors in production or is dropped for an ephemeral Ed25519 key elsewhere. `KeysJSON` wins over `KeysJSONBase64` after trim.
2. `principalFromClaims` says the payload stays a map because `aud` is a string or an array, and `accessTokenPayload` can only emit a string.
3. The guard's non-`ErrInvalidToken` branch says a client token never reaches that 500.

## Steps

- [x] Comment the key-selection rules.
- [x] Comment the claim map.
- [x] Comment the 500 branch.
- [x] `go test ./internal/platform/auth` and `make verify`.

## Decision Log

- 2026-10-11: The four rules sit on the two functions that implement them. `addKey` owns which key signs and which keys are published. `NewKeyRing` owns the JSON-versus-base64 choice and the empty-signer fallback.
- 2026-10-11: B1, B2, and B3 stay out. A comment must not paper over the RSA key that boots and the whitespace issuer that counts as configured.

## Verification

```bash
go test ./internal/platform/auth -count=1
make verify
```

Outcome (2026-10-11): both commands passed. `make verify` full profile passed in 16.931s. No token, JWKS, or startup behavior changed.

## Completion Notes

`NewKeyRing` states that trimmed `KeysJSON` wins, and that a set with no signer errors in production or is dropped for an ephemeral Ed25519 key. `addKey` states that the first private key signs, later private keys only verify, a public-only key is published when another key signs, and a bad key returns without an error. `principalFromClaims` states why the payload is a map. The guard's other-error branch states that a client token never reaches that 500.

## Follow-Ups

- [ ] B1 reject a private key that cannot sign EdDSA, after an explicit yes
- [ ] B3 trim issuer and audience, after an explicit yes
- [ ] B2 stack sentence or an RS256 project, after an explicit choice
