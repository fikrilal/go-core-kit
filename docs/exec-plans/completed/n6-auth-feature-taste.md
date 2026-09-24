# N6 — auth feature taste

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 06
- Related: `_WIP/readability/06-features-auth.md`

## Objective

Move the auth problem table out of the use cases, name the three inline problems, and make the OIDC kind switch exhaustive. Comments record the decisions a junior cannot see from the names.

## Constraints

- Do not reorder `issueSession`, delete the `Refresh` pre-check, or refetch on an OIDC email conflict.
- Do not add a pg repository, an OIDC connect route, or a Me route.
- Deleted login and deleted OIDC exchange still return invalid credentials.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. Problem helpers live in `app/problems.go`. Register, change-password, and `checkPasswordPolicy` call named helpers.
2. `ExchangeOidc` has a default that reports an unexpected kind. The verified arm falls through into the identity checks.
3. The comments listed in note 06 are on the named functions.
4. `auth.Deps.Logger` and its assignment are gone.

## Steps

- [x] Move the helpers and name the three inline problems.
- [x] Add the OIDC default and the listed comments.
- [x] Drop the unused logger.
- [x] `go test ./internal/features/auth/...`.

## Decision Log

- 2026-10-11: An unexpected kind previously fell through into the email-not-verified path. The default reports the kind. No current verifier returns that kind.

## Verification

```bash
go test ./internal/features/auth/... -count=1
```

Outcome (2026-10-11): app and infra tests passed. `TestExchangeOidcUnexpectedKind` calls `ExchangeOidc` with kind `mystery` and requires a non-problem error that names the kind.

## Completion Notes

`checkPasswordPolicy` stays on `Service`. Wire codes are unchanged.

## Follow-Ups

None.
