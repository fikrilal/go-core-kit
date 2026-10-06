# N4 — zero time writes NULL

- Status: completed
- Owner: Core kit maintainers
- Risk: medium
- Date: 2026-10-11
- Roadmap: readability track, note 04 bug B1
- Related: `_WIP/readability/04-platform-db.md`

## Objective

Make `utctime.Time.Value` of the zero time return SQL NULL. Scanning NULL, and scanning the year-1 UTC string, still yields the zero time. Inserting a zero time into a NOT NULL timestamp column fails.

## Constraints

- Do not change the DSN or the migration SQL.
- A non-zero value still uses the fixed-width UTC layout.
- No current query binds a zero time. Nullable columns that receive one now store NULL.

## Impact Areas

- API / contract: no
- DB / migrations: yes
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `Value` of the zero time is nil.
2. `Scan(nil)` and `Scan` of `0001-01-01T00:00:00.000000000Z` are zero.
3. `INSERT` of a zero time into `TEXT NOT NULL` fails.
4. The type comment no longer says the zero time writes year 1.

## Steps

- [x] Return nil from `Value` when `IsZero`.
- [x] Update the type comment.
- [x] Add the value/scan test and the NOT NULL insert test.

## Decision Log

- 2026-10-11: `database/sql` treats a nil `driver.Value` as NULL. The year-1 string remains a scan input so an old row still reads as zero.

## Verification

```bash
go test ./internal/platform/db/utctime ./internal/platform/db -count=1 -v -run 'TestZeroValueIsNullAndYearOneScanIsZero|TestZeroTimeInsertFailsNotNull'
```

Outcome (2026-10-11): `Value(zero)` logged `<nil>`. The year-1 scan was zero. The SQLite insert returned an error. `make verify` exited 0 (full profile, 7.261s).

## Completion Notes

`IsZero` and `Value` now agree on NULL. A NOT NULL column rejects the zero time instead of storing year 1.

## Follow-Ups

None.
