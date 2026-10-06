# N3 — stack sentence is EdDSA only

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 03 bug B2
- Related: `_WIP/readability/03-platform-auth.md`

## Objective

Make `docs/core/project-stack.md` say the kit verifies EdDSA only.

## Constraints

- Do not implement RS256.
- Do not change the verifier.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. The auth bullet no longer says RS256 is supported.
2. It says EdDSA only.

## Steps

- [x] Replace the sentence.

## Decision Log

- 2026-10-11: The code and plan 3.4 already shipped EdDSA only. The stack sentence was the remaining claim.

## Verification

The sentence is the change. `TestVerifyAccessTokenRejections/alg_mismatch` still rejects an RS256 header.

## Completion Notes

`docs/core/project-stack.md` now says "EdDSA only".

## Follow-Ups

- [ ] RS256, if a product needs it, is its own change.
