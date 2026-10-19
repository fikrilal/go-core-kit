# x2 — Export/import helper for cross-engine moves

## Objective

Move data between the two engines without hand-written SQL: a `gokit` command that
reads one lane and writes the other, for the moments a SQLite deployment grows into
Postgres or a replica needs seeding.

## Scope

- **In:** a table-by-table copy over `database/sql` for the tables this kit owns,
  in dependency order, with row counts and a checksum per table reported on both
  sides; a `--dry-run` that reports what would move; refusing to run against a
  target that is not empty unless `--replace` is passed.
- **Out:** schema translation (migrations do that), type coercion beyond what
  `utctime` already guarantees, and partial-table filtering. Those belong to a
  product's own migration, not to a template helper.

## Design

`gokit db export --lane sqlite --out <file>` writes a portable snapshot (JSON lines
per table, values in their stored text form so both engines read them back), and
`gokit db import --lane pg --in <file>` applies it inside one transaction per
table. The command is a thin shell over the generated queries where they exist and
`database/sql` otherwise; it never invents schema.

## Acceptance checks

1. Round trip against the harness: a seeded SQLite database exports, imports into a
   Postgres schema, and every table's count and checksum match.
2. Import refuses a non-empty target without `--replace`, and `--dry-run` writes
   nothing.
3. The lane choice comes from the same `dblane` seam the processes use, so the
   command cannot drift from the runtime.

## Progress — first attempt (2026-10-17)

A first implementation was written and **removed before landing**: the command
(`tools/gokit/dbmove.go`) reached a compiling state but not a lint-clean one, and a
half-built command that fails the gate is worse than none. Nothing was wired into
the dispatcher, so the removal was clean.

Two findings worth keeping:

- **One binary is one engine, so the lanes cannot be flags.** `dblane.Open` opens the
  *compiled* lane, and `//go:build pg` decides which that is. So the commands are
  `gokit db export --out <file>` and `gokit db import --in <file>` with no `--lane`
  option: the SQLite build exports, the pg build imports, and a cross-engine move is
  two invocations of two binaries. The acceptance check has to follow: each lane
  proves the *format* round-trips in its own tests (export then import within the
  same lane, using two harness databases), and the cross-lane move is a documented
  recipe rather than an automated one.
- **The table list is the kit's, not the schema's.** Reading table names portably
  means either `PRAGMA table_info` or `information_schema` — lane-specific by
  definition. A constant list in dependency order keeps the command engine-blind,
  and the cost is that a future migration must add its table there. The doc should
  say so next to the constant.

The next attempt starts from the standard's shape: JSON lines with a versioned
header per table, values as stored text or null, one transaction per table,
`--dry-run`, and a non-empty-target guard with `--replace`.

## Outcome — landed 2026-10-17

`gokit db export --out <file>` and `gokit db import --in <file> [--replace]
[--dry-run]` are implemented (`tools/gokit/dbmove.go`) and wired into the
dispatcher, with the design the failed first attempt had already settled: JSON lines
with a versioned header per table, values as stored text or null, one transaction per
table in dependency order, a non-empty-target refusal, and a dry run that writes
nothing.

**The lane constraint became the design.** There is no `--lane` flag: `dblane.Open`
opens the compiled lane, so the SQLite build exports and the pg build imports, and a
cross-engine move is two invocations of two binaries. The acceptance check follows
that shape — each lane proves the *format* round-trips in its own test
(`TestDBMoveRoundTrip` uses two harness databases, seeds via hand-written SQL through
`dbtest.Rebind`, and asserts the imported pair, the refusal, the dry run, and the
replace) — and the cross-engine move is the documented recipe, since one process
cannot hold both lanes.

The table list is a constant in dependency order rather than schema discovery, with
the cost written down in `docs/standards/database.md`: a new migration adds its table
there.

**Correction (review round 1).** The paragraph above originally claimed the pg lane
ran the round trip. It did not: the test seeded with SQLite `?` placeholders and no
`dbtest.Rebind`, so `make verify-pg` with a live `DATABASE_URL` — the command CI's
`verify-pg` job runs — failed with `syntax error at or near ","`, and the lane only
looked green because the guarded tests skip without a URL. A reviewer reproduced it
against a real server. Both seeds now rebind, and `snapshotValue` renders timestamps
through `utctime.Stored()`: the pg driver hands them back as `time.Time`, and `%v`
wrote the operator's local zone — text pg rejects and this kit's readers cannot
parse. Verified this time by running the lane with a live `postgres:17`; the
cross-engine round trip is still unproven and stays on the follow-up list below.

**Open from that review, not fixed here:** the import order breaks two real foreign
keys (`stored_files` before `user_profiles`; `refresh_tokens.replaced_by_id` needs a
second pass), a failed `--replace` can leave a target with cascaded rows gone because
each table commits separately, unknown tables in a snapshot are dropped silently, the
export truncates its destination before reading, and no counts or checksums are
compared on the way in. Each is recorded with its reproduction in the review round's
findings rather than implied fixed.
