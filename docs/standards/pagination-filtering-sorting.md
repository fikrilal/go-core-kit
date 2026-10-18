# pagination-filtering-sorting

Lists are keyset-paginated, never offset-paginated: an offset drifts when rows are
written between pages, and the snapshot's `nextCursor` is the contract for "carry
on from here".

## Request

`limit` defaults to the route's configured value and is capped by it; the list
config (`httpx.ListQueryConfig`) declares the default, the maximum, and the sorts a
route accepts. An unlisted sort is a field issue, not a silent fallback: the
default sort applies only when the caller asked for nothing.

## Cursor

The cursor is opaque, versioned, and bound to the normalized sort it was issued
for. It carries the last row's key values in their exact stored text form
(`utctime.Stored`), base64url encoded with a version prefix. Garbage, a version
mismatch, a cursor issued for another sort, or a cursor missing a key for that sort
is rejected as a `cursor` field issue with `VALIDATION_FAILED` — a cursor is a
client's own data echoed back, so a bad one is a bad request, not a server fault.

## Query shape

Two-key sorts page with the disjunction a keyset needs:

```sql
WHERE (created_at < :c1 OR (created_at = :c1 AND id < :c2))
ORDER BY created_at DESC, id DESC
LIMIT :limit_plus_one
```

The handler reads `limit + 1` rows: if the extra row exists, `hasMore` is true and
`nextCursor` is built from the last returned row. SQL is written in the repository
and the query file; handlers never build it (`docs/standards/database.md`).

## Hand-built SQL

Where a page query cannot be expressed as a static statement (the me sessions list
is the worked example), it is assembled in the repository and passed through
`db.Rebind`, which converts the portable `?` placeholders to the lane's form. One
implementation per engine, so the query reads the same in both.

## Testing

Contract tests assert both directions: a first page with `hasMore`, a follow-up
page that resumes exactly at the boundary, and the rejection cases for a tampered
or mismatched cursor. The lane's harness gives each test its own database, so
pagination assertions are about ordering rather than leftover rows.
