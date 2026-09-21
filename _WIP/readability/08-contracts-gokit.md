# Section note: contracts allowlist and `tools/gokit`

- Status: Draft, waiting for acceptance
- Date: 2026-10-11
- Scope: `contracts/allowlist.go`, `allowlist.yaml`, `allowlist_test.go`, `provenance.json`, `vacuum.yaml`, `contracts/README.md`, and the hand-written `tools/gokit` commands.
- Out of scope: `contracts/generated.go` and the body of `contracts/backend.openapi.yaml`. Those are generated and pinned. This note reads them only as inputs to the gates.
- Ruler: `_WIP/go-core-kit-readability-proposal.md`
- Code changes: none in this pass
- Earlier notes stay unapplied
- This is the last section in the proposal. The notes are the investigation. Applying them is a later yes.

## Verdict

The reader here is the person running the kit, not the person adding a feature. `gokit` is a command switch plus one function per job. The allowlist is a flat list of operation ids the snapshot declares and this kit does not serve yet. Coverage is not one check. Digest and allowlist shape are `gokit contract check`. "Implemented or explicitly pending" is `TestContractCoverage`, and it runs because `verify` runs `go test`.

Two things read wrong. `ParseAllowlist` is not a YAML parser. The contract-check line "not allowlisted" is not "implemented".

One gate is stale. `plannedPaths` still exempts `internal/features/` after that tree exists.

## What was read

| File | Lines | Role |
| --- | --- | --- |
| `contracts/allowlist.go` | 23 | Line scan of `- id` entries |
| `contracts/allowlist.yaml` | 32 | 25 pending operation ids |
| `contracts/allowlist_test.go` | 18 | Comments, groups, inline notes |
| `contracts/provenance.json` | 7 | Source repo, commit, sha256 |
| `contracts/vacuum.yaml` | 13 | Spectral-shaped rules for vacuum |
| `contracts/README.md` | 21 | Pin workflow |
| `tools/gokit/main.go` | 35 | Exit code and signal |
| `tools/gokit/command.go` | 208 | Parse and dispatch |
| `tools/gokit/verify.go` | 178 | Profiles, steps, drift |
| `tools/gokit/contract.go` | 270 | Sync, digest, allowlist, codegen |
| `tools/gokit/gates.go` | 251 | Honesty fixtures |
| `tools/gokit/knowledge.go` | 261 | env.example, map, links, ADRs |
| `tools/gokit/doctor.go` | 178 | Local tool versions |
| `tools/gokit/dbgenerate.go` | 122 | sqlc into `internal/db/gen` |

`contracts/backend.openapi.yaml` was not reviewed as prose. A probe counted its operations. Tests were read where they lock a parser (`allowlist_test.go`, `knowledge_test.go`, `doctor_test.go`, the provenance cases in `contract_test.go`).

## Hard to read

### 1. `ParseAllowlist` is a line scan

```go
if !strings.HasPrefix(line, "- ") {
    continue
}
```

It keeps any line whose trimmed text starts with `- `. It strips a trailing `#` comment. It does not read the `operations:` key, indentation, or quotes. `contracts` stays free of a YAML library. The committed file is only that shape, and `TestParseAllowlist` locks comments and an inline note.

A second YAML list in the same file would be treated as more operation ids. A quoted id would keep the quotes and then fail `checkAllowlist` as "not in the snapshot". Say this on the function. Do not replace it with a parser.

### 2. "Not allowlisted" is not "implemented"

`runContractCheck` prints:

```text
allowlist: %d pending, %d not allowlisted, %d declared
```

The middle number is `len(snapshot ids) - len(allowlist)`. On this pin that is 9. Those 9 happen to be the routes `cmd/api` registers, but this function never looks at the router. An operation removed from the allowlist and not registered still counts as "not allowlisted", and this command still prints `ok`.

`TestContractCoverage` is the check that fails that case. It runs inside the `test` step of every profile, not inside the `contract` step. The README already says that. The log line does not.

Reword the line so the middle count is "snapshot ids outside the allowlist". One comment on `checkAllowlist`: duplicates and unknown ids only; route coverage is the test.

`snapshotOperationIDs` is the same kind of scan: a line that matches `operationId:`. Probed 2026-10-11 against kin-openapi on the pinned file: both lists are 34 ids, neither has an id the other lacks. `TestContractCoverage` uses the real loader. Leave the regex. The comment should say the two readers agreed on this snapshot, and the test is the one that knows methods and paths.

### 3. The step budget does not fail the step

`runStep` prints a warning when a step takes longer than `budget` and still returns nil. `defaultStepBudget` is 2 minutes. Knowledge and contract use 30 seconds. Nothing cancels the command. The word `budget` reads like a deadline. Say "advisory" on the field.

`ciSteps` returns `fullSteps()`. The comment names phase 7. Three profile names, two lists. Leave it.

## Correct Go that should stay

- `parseCommand` is the function to copy. One switch, unknown arguments are a `usageError`, and `main` turns that into exit 2 plus the usage text. Every other error is exit 1. `os.Exit(runMain())` is the same split as `cmd/api`: defers in `runMain` run before the exit.
- `doctor` is not a verify step. Version skew warns. A missing tool fails. `verify` never calls it.
- `contract sync` refuses an untracked or dirty source, then writes the snapshot and the provenance. The two writes are not one transaction. A crash between them fails the next digest check, and a re-sync repairs it. Sync does not edit the allowlist. New snapshot ids fail `TestContractCoverage` until someone implements them or adds them. That is the point.
- `writeFileAtomic` writes a temp file in the destination directory and renames it.
- `fast` is knowledge, contract, fmt, vet, lint, test. `full` adds both builds, drift, vacuum, and gates honesty. Vacuum uses `contracts/vacuum.yaml`, including the rule that every operation has `x-error-codes`.
- Drift regenerates sqlc and oapi-codegen, then `git status --porcelain` on `internal/db/gen` and `contracts/generated.go`. Untracked output says "not committed". A diff says "out of date". `.gen/` is scratch and gitignored.
- `generateDB` deletes stale `sqlite_` and `pg_` files, prefixes the new ones, and writes the build tag itself. It calls `sqlc` with `context.Background()`, not the process context. An interactive Ctrl-C still reaches `sqlc` because the child is in the same process group. Leave the call.
- The honesty suite must see a failure that contains a marker. A green lint is a failed scenario. The closure over `violationFixtures` is safe on this module's Go version (1.22 or newer; `go.mod` says 1.27.2).
- The coverage injection inserts `honestyFakeOperation` immediately before the root `info:` key. On this snapshot `paths:` comes first and `info:` is the next root key, so the inserted block is another path. Probed: kin-openapi loads the doctored file and sees `gates.honesty.fake`. If a future pin moves `info:` and the anchor is gone, honesty fails with "anchor not found" instead of passing. Fail closed. Do not move the anchor in this pass.
- Redis driver confinement is a depguard rule and is not one of the honesty fixtures. Do not add that fixture here.
- `plannedPaths` may exempt `cmd/worker/` and `internal/shared/`. They are in the AGENTS map and not on disk. Knowledge checks markdown links under `docs/` plus `AGENTS.md` and `README.md`. It does not walk `_WIP/`. Backtick paths are checked only in `docs/README.md` and `docs/adr/README.md`.

## Real bugs

### B1. `internal/features/` is still exempt

```go
var plannedPaths = []string{"cmd/worker/", "internal/features/", "internal/shared/"}
```

The comment says to shrink the list as phases ship. `internal/features/auth` is on disk, and the AGENTS map lists `internal/features/`. The existence check `continue`s before `os.Stat` for every planned path. Deleting that directory would not fail `knowledge check`.

Remove `internal/features/` from the slice. The directory is there, so the check still passes. `cmd/worker/` and `internal/shared/` stay exempt.

## Design question

What belongs in `gokit`, and what stays a test?

`gokit` owns the repository gates: pin, digest, allowlist shape, generation, drift, spec lint, knowledge, and the proof that a gate can fail. The feature does not import it.

Route coverage stays in `cmd/api` `TestContractCoverage` because it has to boot `newServer` and read the registered operations. Pulling that into `gokit` would make the tool import the API. Do not.

`contracts.ParseAllowlist` stays the shared reader. `gokit` and the coverage test both call it. A second parser would be the drift.

## Function notes

### `allowlist.go` and `allowlist.yaml`

Finding 1. The YAML groups are for humans: phase 3 auth, phase 4 me, admin, merchant onboarding. 25 ids. The scanner does not care about the headings.

### `contract.go`

Finding 2. `validateSnapshot` only requires an `openapi: 3.x.y` line and a `paths:` line. Vacuum and kin-openapi are the real schema checks.

### `verify.go`

Finding 3. `runProfile` trusts `parseVerify` to reject an unknown name and checks again anyway. Leave the second check.

### `gates.go`

The fixture files are deleted by the defer, including the parent directory created for the run. A killed process can leave `internal/features/violation` or `internal/platform/violation` behind. The defer is best-effort. Do not make this more elaborate.

### `knowledge.go`

Bug B1. `configEnvKeys` reads `env` struct tags with `reflect`, so a new config field fails the check until `env.example` names it. That is the right coupling.

### `doctor.go` and `dbgenerate.go`

Correct-Go list. `compareVersions` treats a non-numeric piece as 0 via `Atoi`. `go version` does not produce that shape. Leave it.

### `command.go` and `main.go`

The dispatch switch. Same `runMain` comment as `cmd/api`, one line.

## What acceptance authorizes

Taste:

1. Comment on `ParseAllowlist`: line scan, any `- ` item, `#` comments, no YAML types.
2. Reword the contract-check count to "snapshot ids outside the allowlist", and comment that route coverage is `TestContractCoverage`.
3. Comment on `snapshotOperationIDs`: text scan; it matched the OpenAPI loader on this snapshot (34 ids).
4. Comment that `step.budget` is advisory and does not cancel.
5. Comment on the coverage-injection anchor: the fake path is inserted before the root `info:` key.
6. The one-line `runMain` / `os.Exit` comment, same meaning as `cmd/api`.

Bugs, separate yes:

- B1. Remove `internal/features/` from `plannedPaths`.

Not in this acceptance:

- A YAML library for the allowlist.
- Folding `TestContractCoverage` into `gokit`.
- Passing a context into `sqlc`, or a redis-driver honesty fixture.
- Changing the `ci` profile.
- Applying the earlier notes.

## Verification

No production edit. `make verify` was not run.

Probe, deleted afterward (`tools/gokit/probe_audit_test.go` is not in the tree):

```bash
go test ./tools/gokit -run TestAuditContractProbe -count=1 -v
```

Result: pass. Regex ids 34, loader ids 34, no id on only one side. The doctored snapshot loads, and `gates.honesty.fake` is present. The probe is not coverage.
