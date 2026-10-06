# N3 — trim issuer and audience

- Status: completed
- Owner: Core kit maintainers
- Risk: medium
- Date: 2026-10-11
- Roadmap: readability track, note 03 bug B3
- Related: `_WIP/readability/03-platform-auth.md`

## Objective

Trim issuer and audience before the production-empty check and before either value is stored.

## Constraints

- A non-production empty issuer still skips the `iss` check.
- Do not change access-token claim names or the EdDSA algorithm.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: yes
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. Whitespace-only issuer and audience fail `NewVerifier` when `productionLike` is true, the same as empty strings.
2. A non-production verifier built with a whitespace issuer skips the `iss` check.
3. `NewIssuer` stores the trimmed values, so a padded constructor still matches a verifier built from the trimmed strings.

## Steps

- [x] Trim in `NewVerifier` and `NewIssuer`.
- [x] Add `TestNewVerifierTrimsIssuerAndAudience`.

## Decision Log

- 2026-10-11: Trimming only the verifier would sign a padded issuer and then reject it. Both constructors store the trimmed strings.

## Verification

```bash
go test ./internal/platform/auth -count=1 -v -run TestNewVerifierTrimsIssuerAndAudience
```

Outcome (2026-10-11): whitespace production-like values failed the empty check. A blank non-production issuer accepted a token whose `iss` was a different URL. A token signed from padded issuer and audience verified against the trimmed pair. `make verify` exited 0 (full profile, 7.261s).

## Completion Notes

`AUTH_ISSUER=" "` no longer boots a production-like process.

## Follow-Ups

None.
