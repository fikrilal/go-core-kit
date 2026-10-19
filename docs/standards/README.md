# Standards (normative)

Standards are normative. If you need to deviate, add an ADR and document the exception.

The wire-contract behaviors are owned by backend-core-kit's standards. This kit mirrors them; the standards below are adapted here as the corresponding platform code lands. Until a standard appears in this directory, backend-core-kit's `docs/standards/` is authoritative for its topic.

| Standard | Status | Lands with |
| --- | --- | --- |
| api-response | in place | landed with the platform, auth, worker, and dual-lane phases |
| error-codes | in place | landed with the platform, auth, worker, and dual-lane phases |
| authentication | in place | landed with the platform, auth, worker, and dual-lane phases |
| pagination-filtering-sorting | in place | landed with the platform, auth, worker, and dual-lane phases |
| configuration | in place | landed with the platform, auth, worker, and dual-lane phases |
| database | in place | landed with the platform, auth, worker, and dual-lane phases |
| queues-jobs | in place | landed with the platform, auth, worker, and dual-lane phases |
| observability | in place | landed with the platform, auth, worker, and dual-lane phases |
| duplication | in place | landed with the cross-cutting follow-ups |
| testing-strategy | in place | landed with 2.3 (contract suite harness) |
| ci-cd | in place | landed with the platform, auth, worker, and dual-lane phases |

Known gaps and their blast radius are recorded in
[`docs/engineering/residual-risk.md`](../engineering/residual-risk.md).
