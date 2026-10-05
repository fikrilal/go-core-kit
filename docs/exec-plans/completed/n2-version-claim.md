# N2 — version claim

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 02 bug B2
- Related: `_WIP/readability/02-platform-primitives.md`

## Objective

Stop `version` from claiming that the build injects `Version` and `Commit`.

## Constraints

- The package stays.
- Do not add `-X` ldflags or a startup log in this change.
- The default values stay `dev` and `none`.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `internal/platform/version` still exists.
2. Neither exported comment says the value is injected at build time.

## Steps

- [x] Delete the injection sentences.

## Decision Log

- 2026-10-11: No caller reads the variables, and no build flag sets them. Deleting the sentence is the claim that matches the tree. Wiring ldflags is a later choice.

## Verification

The package has no tests. `go test ./internal/platform/version` reports no test files. The comments are the change.

## Completion Notes

`Version` is "the build's semantic version". `Commit` is "the build's git revision".

## Follow-Ups

- [ ] Wire `-X` ldflags and a caller when a binary needs to report the revision.
