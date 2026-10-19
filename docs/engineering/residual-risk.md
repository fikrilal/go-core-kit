# Residual risk log

Known gaps, each with where it is felt and what would close it. This is a log, not a
backlog: the roadmap owns planned work, and this file records what is currently true
and unfixed.

| Risk | Where it bites | Closing it |
| --- | --- | --- |
| Redis is a single point for rate limits, idempotency, and the queue | Losing Redis empties limiter windows (clients can retry into a gap) and drops pending tasks | A managed Redis with persistence; scheduled deletion re-arms from the stored schedule, emails are re-requestable |
| Serializable retry reruns a caller's closure | A pg transaction body may execute twice | Callers keep writes inside the transaction (documented in `docs/standards/database.md`); a side effect outside it would need an outbox |
| Rate-limit thresholds are unbenchmarked | Limits are production defaults, not measured | 9.2's performance baseline |
| No metrics or tracing exporter | Only logs and request ids exist | Decide whether the kit owns exporters or leaves that to deployments (`docs/standards/observability.md`) |
| Idempotency has no payload fingerprint | A replayed key with a different body returns the first response | A fingerprint column on the idempotency record |
| The pg lane has no `SELECT ... FOR UPDATE` helper | Concurrent last-admin checks rely on serializable retry | A documented locking helper if a flow needs it |
| FCM application-default credentials are unported | Deployments must pass a service-account JSON | Cloud-native credential resolution |
| Worker tests stay on the SQLite lane | `cmd/worker/main_test.go` is still `!pg` | A lane-aware worker harness, like the one `dbtest` gives the contract tests |
| Backups are documented, not automated | Litestream is a runbook (`docs/engineering/single-box-deployment.md`) | Deployment-owned; the kit has no operator |
| The shape oracle ignores the HTTP method and never records a problem code | `GET` and `PATCH` on one path share a capture, and a wrong error code is not caught | Include the method in the key; capture refusals |
| The oracle's `States()` can drift from its matrices | A row added to a matrix is dead data unless `States()` is edited too | Derive one from the other, or assert the key sets match |
| `db import` does not verify counts against the snapshot | A snapshot that is complete but stale imports as a valid subset | Compare per-table row counts with the export's report |
| `db export` reads table by table outside a snapshot | On a live database, a child table can be captured at a different instant than its parent | Stop writes for a consistent snapshot, or export inside one transaction |
| The admin and merchant surfaces are not carried | Clients needing them cannot use this kit | ADR-0012 records the decision; a fork that needs them carries them |
