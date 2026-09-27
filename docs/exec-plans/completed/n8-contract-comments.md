# N8 — contract comments and feature map

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 08
- Related: `_WIP/readability/08-contracts-gokit.md`

## Objective

Say what the allowlist scan, the operation-id scan, the step budget, and the honesty anchor actually do. Stop exempting `internal/features/` from the project map.

## Constraints

- `cmd/worker/` and `internal/shared/` stay exempt.
- Do not add a YAML parser, fold `TestContractCoverage` into gokit, or change the `ci` profile.
- The contract-check count is snapshot ids minus allowlist ids. It still does not inspect the router.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `ParseAllowlist` says it is a line scan.
2. The contract-check line says "snapshot ids outside the allowlist" and points route coverage at `TestContractCoverage`.
3. `snapshotOperationIDs`, `step.budget`, the honesty anchor, and `runMain` carry the listed comments.
4. `plannedPaths` is `cmd/worker/` and `internal/shared/` only.

## Steps

- [x] Add the comments and the count wording.
- [x] Remove `internal/features/` from `plannedPaths`.
- [x] `go test ./tools/gokit`.

## Decision Log

- 2026-10-11: Auth has shipped, so a missing `internal/features/` directory must fail the knowledge check. The worker and shared trees still do not exist.

## Verification

```bash
go test ./tools/gokit -count=1
```

Outcome (2026-10-11): gokit tests passed, including `TestPlannedPathsLeaveShippedFeaturesChecked` and `TestKnowledgeCheckOnRepository`.

## Completion Notes

The knowledge gate now stats `internal/features/`. The other two planned paths stay exempt.

## Follow-Ups

- [ ] `cmd/worker/` leaves `plannedPaths` when the worker lands.
- [ ] `internal/shared/` leaves `plannedPaths` when that tree lands.
