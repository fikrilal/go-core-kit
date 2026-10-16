# Background jobs

Background work rides `internal/platform/queue`, which wraps asynq over
`REDIS_URL` — the same value the API uses. `cmd/worker` is the composition root
for jobs; `cmd/api` only schedules them.

## The lane

- Queues are named by scope: `default`, `emails`, `push`, and `users`. A worker
  polls only the queues its `asynq.Config.Queues` map lists, so a new lane means a
  new entry there — a registered handler on an unpolled queue never runs.
- Job names are platform constants (`email.Queue`, `push.JobSend`,
  `meapp.JobFinalizeDeletion`), never string literals at the call site.
- Every payload travels inside the `queue.Meta` envelope, carrying `request_id`
  and `trace_id` so a job's log line can be joined to the request that caused it.
  `queue.Bind` decodes typed payloads and hands the handler the meta.

## Writing a handler

Handlers live in the feature's `app` package and are registered in `cmd/worker`
only when their collaborator is configured: the email handlers need a sender, the
push handler needs FCM credentials, the stored-file cleanup needs object storage.
The finalize handler is always registered.

Skips are successes, not failures. A missing user, an already-deleted account, a
revoked session, or a token that no longer matches is an outcome the handler
reports and logs at `warn` — returning an error would put asynq into a retry loop
for a state that will not change. Real failures return an error and let the retry
policy apply (five attempts, 30 s timeout, jittered exponential backoff).

## Delayed work

`queue.Scheduler` enqueues a job for a future instant under a deterministic task
id (`finalize:<userId>`), removing any pending task first so request/cancel cycles
reuse the id, and cancels by the same id. A job that fires before it is due
re-arms itself for the stored instant rather than polling; the last-admin guard
re-checks after a day.

Producing is best effort where the state is already committed (deletion emails),
and propagated where it is not (the finalize arm itself). Retries re-arm from the
stored schedule, so a transient Redis failure cannot strand an account.

## Testing

The queue package tests run against `miniredis`, and the worker tests execute the
composed handlers through `queue.Bind` against a real SQLite database — that is
how the closures, the payload envelope, and the repository calls stay covered
without a live Redis.
