# queues-jobs

Background work rides `internal/platform/queue` (asynq over `REDIS_URL`), and
`cmd/worker` is its composition root. `cmd/api` schedules; the worker consumes.

## Names and lanes

Queues are named by scope: `default`, `emails`, `push`, `users`. A worker polls only
the queues its config lists, so a new lane means a new entry there — a handler on an
unpolled queue never runs. Job names are constants next to their feature, never
literals at a call site.

## Payloads

Every payload travels inside the `queue.Meta` envelope, which carries `request_id`
and `trace_id` so a job's log line joins the request that caused it. `queue.Bind`
decodes a typed payload and hands the handler the meta; handlers take
`(ctx, payload, meta)` and never touch asynq types.

## Registration follows configuration

A handler is registered only when its collaborator exists: email handlers need a
sender, the push handler needs FCM credentials, the stored-file cleanup needs object
storage. The finalize job needs only a database, so it is always registered.

## Skips are successes

A missing user, an already-deleted account, a revoked session, a token that no
longer matches, a file already gone: these are outcomes the handler reports and logs
at `warn`. Returning an error would put the queue into a retry loop for a state that
will not change. Real failures return an error and take the retry policy (five
attempts, 30-second timeout, jittered exponential backoff).

## Delayed work

`queue.Scheduler` enqueues for a future instant under a deterministic task id
(`finalize:<userId>`), removing any pending task first so request/cancel cycles reuse
the id, and cancels by the same id. A job that fires before it is due re-arms itself
at the stored instant rather than polling; the last-admin guard re-checks after a
day. Producing is best effort where the state is already committed (deletion emails)
and propagated where it is not (the finalize arm itself), and retries re-arm from the
stored schedule so a transient Redis failure cannot strand an account.

## Testing

The queue's own tests run against `miniredis`; the worker tests execute the composed
handlers through `queue.Bind` against a real database. The pg lanes run the same
tests against a live server in CI (`make verify-pg`).
