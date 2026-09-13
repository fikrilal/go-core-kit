# Pinned contract

`backend.openapi.yaml` is a byte-for-byte copy of backend-core-kit's committed `docs/openapi/openapi.yaml`, taken from the revision recorded in `provenance.json`. backend-core-kit owns this contract; this kit follows it and never changes it locally.

## Files

| File | Purpose |
| --- | --- |
| `backend.openapi.yaml` | Pinned snapshot — the wire contract this kit serves |
| `provenance.json` | Source repository, commit, path, and SHA-256 of the snapshot |
| `generated.go` | Generated request/response types (oapi-codegen, models only) |
| `allowlist.yaml` | Snapshot operations not implemented yet, grouped by phase; enforced by the coverage test |
| `vacuum.yaml` | Spec-lint ruleset mirroring backend-core-kit's `.spectral.yaml` |

## Workflow

- Verify the pin: `gokit contract check` (digest + allowlist validity; runs in the fast profile).
- Coverage: `cmd/api`'s `TestContractCoverage` proves (implemented ∪ allowlisted) equals the snapshot, with method and path parity per operation.
- Lint: `gokit verify --profile full` runs `vacuum` against the snapshot with the error-severity rules the origin enforces.
- Re-pin after a contract bump: `gokit contract sync --source <backend-core-kit>/docs/openapi/openapi.yaml`, then `make generate` and commit both the snapshot and regenerated types.
- `contract sync` refuses sources that are untracked or have uncommitted changes, so the provenance always names a real revision.
