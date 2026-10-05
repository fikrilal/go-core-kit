# N3 — reject non-EdDSA keys

- Status: completed
- Owner: Core kit maintainers
- Risk: medium
- Date: 2026-10-11
- Roadmap: readability track, note 03 bug B1
- Related: `_WIP/readability/03-platform-auth.md`

## Objective

Fail `NewKeyRing` when a configured key cannot sign EdDSA, before any request is served, and do not publish that key with `alg` EdDSA.

## Constraints

- Do not implement RS256.
- Do not merge `ephemeralKeyRing` into `addKey`.
- An Ed25519 private key still signs.
- A key with no `kid` is still skipped.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: yes
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. A production-like ring built from an RSA private JWK returns an error.
2. That call does not return a ring whose JWKS stamps the key `alg` EdDSA.
3. An Ed25519 private key still signs an access token.
4. An RS256 token is still `ErrInvalidToken`.

## Steps

- [x] Reject a key that does not export as Ed25519.
- [x] Add `TestKeyRingRejectsRSAPrivateKey` beside the production-like key test.

## Decision Log

- 2026-10-11: `jwk.Key` has no `Raw`. `jwk.Export` into `ed25519.PrivateKey` or `ed25519.PublicKey` is the check. A failure returns from `NewKeyRing`, so the key is never published.

## Verification

```bash
go test ./internal/platform/auth -count=1 -v -run 'TestKeyRingRejectsRSAPrivateKey|TestSignAccessTokenClaimsAndHeaders|TestVerifyAccessTokenRejections'
```

Outcome (2026-10-11): the RSA production-like ring returned an error and a nil ring. The Ed25519 sign test passed. The existing "alg mismatch" case still rejects an RS256 header. `make verify` exited 0 (full profile, 7.261s).

## Completion Notes

A mixed set that contains an RSA key also fails the constructor. Non-production no longer turns that set into an ephemeral key.

## Follow-Ups

RS256 remains unsupported.
