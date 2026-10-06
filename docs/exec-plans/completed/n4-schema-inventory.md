# N4 — schema name inventory

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 04 bug B2
- Related: `_WIP/readability/04-platform-db.md`, ADR 0005

## Objective

Compare table and column names in the sqlite and pg goose Up sections. Pass on the current trees. Fail when a name exists on only one side.

## Constraints

- Compare names only. `TEXT` versus `TIMESTAMPTZ` is not a difference.
- Do not add a pg pool, `verify-pg`, or the CI matrix. Those stay task 7.2.
- The comparison is a pure function called from `go test ./db/migrations`.

## Impact Areas

- API / contract: no
- DB / migrations: yes
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: yes

## Acceptance Criteria

1. The embedded sqlite and pg Up trees report no name differences.
2. A one-sided table and a one-sided column are reported.
3. Down sections are ignored, so the SQLite `users_old` rebuild is not a table.

## Steps

- [x] Parse `CREATE TABLE` and `ALTER TABLE ... ADD COLUMN` from the Up section.
- [x] Test the real trees and a synthetic one-sided pair.

## Decision Log

- 2026-10-11: The checker lives next to the embedded trees. `make verify` already runs `go test ./db/migrations`.

## Verification

```bash
go test ./db/migrations -count=1 -v -run TestSchemaInventory
```

Outcome (2026-10-11): the embedded trees reported 0 issues. The synthetic pair reported `users.email` and `sessions` only on the left, and `users.role` and `tokens` only on the right. `make verify` exited 0 (full profile, 7.261s).

## Completion Notes

This is the name check. Engine parity in CI is still phase 7.

## Follow-Ups

- [ ] Task 7.2 runs this check on the Postgres lane in CI.
