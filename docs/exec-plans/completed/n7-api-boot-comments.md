# N7 — api boot comments

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 07
- Related: `_WIP/readability/07-cmd-api.md`

## Objective

Name the 5 second header timeout and record the boot facts that the function names hide.

## Constraints

- `headerTimeout` stays `5 * time.Second`. `shutdownTimeout` stays 10 seconds.
- No `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, or ping-on-boot.
- The logger field deletion is the same edit as N6.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: no
- CI / release: no

## Acceptance Criteria

1. `headerTimeout` sits next to `shutdownTimeout` and is the server's `ReadHeaderTimeout`.
2. The comments listed in note 07 are present.
3. `newServer` does not assign `auth.Deps.Logger`.

## Steps

- [x] Name the timeout and add the comments.
- [x] `go test ./cmd/api -count=1`.

## Decision Log

- 2026-10-11: The header timeout already held `Shutdown` indirectly because slow header reads keep the connection. The comment records that. The value did not change.

## Verification

```bash
go test ./cmd/api -count=1 -run TestServeServesProbesAndShutsDown
```

Outcome (2026-10-11): the existing probe test passed. `/health` stays 200 with body status `ok`.

## Completion Notes

Comments and one named constant. No new route.

## Follow-Ups

None.
