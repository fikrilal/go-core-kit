# observability

## Correlation

`X-Request-Id` is the spine. It is accepted and echoed on every HTTP response,
carried into problems as `traceId`, attached to every log line, and embedded in job
payloads (`queue.Meta`) and audit rows so a request can be followed into the worker
and back out. A request that arrives without one gets a generated id.

## Logging

`slog` with a JSON handler, level from configuration, one logger built at startup
and passed down. Features log through the logger they were given; there is no
package-level logger to reach for. Job handlers log the same ids the request had,
plus the outcome they decided (`sent`, `skipped`, `finalized`, `not_due`) and the
provider's message id when a send succeeded.

## Levels

Outcomes at `info`, skips and refusals at `warn`, failures at `error`. A skip is not
noise: "the account was already deleted" is the answer to "why didn't this email
go out", so it is logged with its reason rather than swallowed.

## Health

The API serves `/health` (process is up) and `/ready` (dependencies answer). The
worker serves the same pair on `WORKER_HTTP_PORT`, because a worker that cannot
reach Redis or the database is not ready to take work. Both drain on
`SIGINT`/`SIGTERM`: the HTTP server stops accepting, and the worker stops picking up
tasks before exiting.

## Not here yet

No metrics endpoint, no tracing exporter, and no log shipping — this standard
records the correlation contract that those will build on, not a collector. The
gap is tracked in `docs/engineering/residual-risk.md`.
