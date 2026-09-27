# N2 — nested redaction

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 02 bug B1
- Related: `_WIP/readability/02-platform-primitives.md`

## Objective

Stop a secret from leaving the process when it sits under a sensitive key inside a map, a group, or `slog.Any`.

## Constraints

- Top-level key redaction stays.
- Message text is not scanned.
- A camelCase key such as `accessToken` is not added to the list.
- The sensitive-key list is unchanged.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no
- Observability: yes
- CI / release: no

## Acceptance Criteria

1. `slog.Any` holding `map[string]any{"password":"hunter2"}` does not emit `hunter2`.
2. `slog.Any` holding `map[string]string{"token":"hunter2"}` does not emit `hunter2`.
3. A log message that contains `hunter2` still contains it.
4. The redactor comment states that message text is not scanned.

## Steps

- [x] Walk group, map, and `slog.Any` values in `redact`.
- [x] `go test ./internal/platform/logging -run TestRedaction -count=1`.

## Decision Log

- 2026-10-11: `ReplaceAttr` already sees `slog.Group` members. `slog.Any` is encoded as JSON and was the leak. Walking the concrete map fixes that without scanning the message.

## Verification

```bash
go test ./internal/platform/logging -count=1 -v -run 'TestRedaction$'
go test ./internal/platform/logging -count=1 -v -run TestRedactionWalksNestedValues
```

Outcome (2026-10-11): both tests passed. The nested log line contains `[REDACTED]` and does not contain `hunter2`. The following message line still contains `hunter2`. `make verify` exited 0 (full profile, 7.261s).

## Completion Notes

Nested maps and string maps are redacted by key. Free-text messages are unchanged.

## Follow-Ups

None.
