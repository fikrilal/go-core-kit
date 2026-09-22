# N2 — health probes

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 02
- Related: `_WIP/readability/02-platform-primitives.md`

## Objective

Make a failing readiness check log on the request logger, and make the probe body go through `httpx` without changing the JSON a probe client sees.

## Constraints

- Do this only after `n2-config-redact`.
- `/health` stays `200` `{"status":"ok"}`. A failed `/ready` stays `503` `{"status":"unavailable","checks":{...}}`. A passing `/ready` stays `200` `{"status":"ok"}`.
- `httpx.Document` stays a `200` writer. JWKS keeps calling it. Do not pass a `503` through `Document`.
- The real check error stays in the log. The body stays the word `unavailable`.
- Do not teach health about Redis or SQLite. Checks stay functions passed from `cmd/api`.
- Do not split `config.Config`, wrap `slog`, or add `DATABASE_URL`.

## Impact Areas

- API / contract: no (status and body unchanged)
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: yes (readiness warning gains `request_id`)
- CI / release: no

## Acceptance Criteria

1. `handleReady` logs the failure with `httpx.Logger(r)`. The line includes the request id.
2. `Handler` no longer stores a logger. `health.New` takes only checks. `cmd/api` and the health tests compile against that signature.
3. Probe writes go through a status-taking `httpx` helper. `Document` still writes `200` by calling that helper.
4. Existing probe tests still pass. One assertion shows the warning carries the request id sent on the request.

## Steps

- [x] Add the status-taking JSON helper and point `Document` at it.
- [x] Log readiness failures with `httpx.Logger(r)` and drop the stored logger.
- [x] Update `cmd/api` and the health tests.
- [x] Assert `request_id` on the failing-check warning.
- [x] `go test ./internal/platform/health ./internal/platform/httpx ./cmd/api` and `make verify`.

## Decision Log

- 2026-10-11: `Document` hardcodes `200`. Using it for `/ready` would turn a down dependency into a live probe. The new helper takes the status. `Document` remains the `200` name JWKS already uses.
- 2026-10-11: A marshal failure on this helper becomes a problem response, which `writeStatus` used to ignore. `statusResponse` is a string and a map, so that branch does not run for these probes.

## Verification

```bash
go test ./internal/platform/health ./internal/platform/httpx ./cmd/api -count=1
make verify
```

Outcome (2026-10-11): both commands passed. `make verify` full profile passed in 26.6s. Probe status and body stayed `200 {"status":"ok"}` and `503` with `unavailable` checks.

## Completion Notes

`httpx.WriteDocument` writes a non-enveloped JSON document at a caller-chosen status. `Document` calls it with `200`. Readiness failures log through `httpx.Logger(r)`, so the warning carries `request_id`. `health.New` takes only checks. `Handler` no longer stores a logger. The check error stays in the log. The body stays the word `unavailable`.

## Follow-Ups

- [ ] Nested redaction, after an explicit yes
- [ ] `version` comment, ldflags, or package deletion, after an explicit yes
