# x5 — `gokit clone`

## Objective

Start a new project from this kit with one command, as cf-core-kit does with
`bin/clone.mjs`: the kit's value intact, the kit's own records gone, and a fresh
history of its own.

## Design

`gokit clone <dir> --module <path> [--name <name>] [--dry-run]`:

- **Strip list as data** — `.git`, `_WIP`, `data`, `.env`, `.cache`, build outputs, and
  `docs/exec-plans`. Nothing outside the list is removed.
- **Go-aware rewrite** — the module path in `go.mod` and every import; the kit's name
  in README, AGENTS, env.example and the docs; links to the stripped records become
  plain text, and the docs index loses the sections that named them.
- **Fresh history** — `git init -b main` plus one commit, so no kit history and no
  rewritten dates travel.
- **Provenance banner** in the README naming the source revision and date.
- **Dry run prints the plan** (strip list, files to rewrite, target) and writes nothing.
- **Guards** — malformed module path, non-empty target, and a destination overlapping
  the kit are all refused.

## Acceptance checks

1. A generated project passes `make verify` untouched.
2. The dry run writes nothing; the refusals leave no partial directory.
3. The generated tree carries no kit history, and exactly one commit.

## Outcome — landed 2026-10-20

Implemented in `tools/gokit/clone.go`, wired into the dispatcher, and covered by three
tests: generation (module and imports rewritten, plans stripped, banner present,
`docs/README.md` cleaned of the stripped sections, exactly one commit), the dry run,
and the three refusals.

**Acceptance check 1 is not yet proven.** Generating a project revealed that the docs
linked into `_WIP` and `docs/exec-plans`, so the knowledge check failed on dangling
targets; the neutralisation for both is in place, but `make verify` has not been re-run
inside a fresh clone since. Checks 2 and 3 are proven by the tests above.
