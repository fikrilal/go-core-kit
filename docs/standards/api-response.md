# api-response

The snapshot owns the shapes; this standard records how they are produced.

## Success envelopes

- Single resources answer `{ "data": ... }`.
- Lists answer `{ "data": [...], "meta": { "nextCursor"?, "limit", "hasMore" } }`; the cursor
  is present only when more rows exist.
- `201` is for creation with a body, `204` for accepted work with nothing to say.
  A `204` carries no body at all.
- Content is JSON unless the operation is a document exchange.

## Producing them

Features answer through `internal/platform/httpx` — `httpx.Data`, `httpx.List`,
`httpx.Created`, `httpx.NoContent`, `httpx.WriteError`. Writing JSON by hand,
`w.Write` in a feature, or `encoding/json` response writes are lint failures by
construction: the dependency rule rejects the import inside `internal/features/**`,
so the envelope cannot drift per endpoint.

## Problems

Errors are RFC7807 problem details: `application/problem+json` with `type`,
`title`, `status`, `code`, `traceId`, and `errors[]` when a request failed
validation per field. `code` is the machine-readable contract; `title` is prose a
client may ignore. See `docs/standards/error-codes.md`.

## Request correlation

`X-Request-Id` is accepted from the caller and echoed on every response, including
problems and `204`s; the problem's `traceId` carries the same value. When the
caller sends nothing, the server generates one. Logs, jobs, and audits record the
same id, so one request can be followed through the worker.

## Idempotency

Retryable writes honor `Idempotency-Key`. A replay returns the stored response with
`Idempotency-Replayed: true`; a retry that arrives while the first is still running
answers `409 IDEMPOTENCY_IN_PROGRESS` rather than running the work twice. Routes
opt in with `router.Idempotent`, or `router.IdempotentFor` with a per-route window
where the operation needs one (the profile-image upload uses its presign TTL).
