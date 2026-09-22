# N2 — config order and redact limit

- Status: completed
- Owner: Core kit maintainers
- Risk: low
- Date: 2026-10-11
- Roadmap: readability track, note 02
- Related: `_WIP/readability/02-platform-primitives.md`

## Objective

Make `config.Validate` name a stable auth field when several integers are invalid, and stop two comments from overclaiming. `ProductionLike` must say who enforces secrets. `redact` must say that only the attribute key is covered.

## Constraints

- Do not move JWK, issuer, or audience checks into `config`. They stay in `platform/auth` `NewKeyRing` and `NewVerifier`.
- Auth integer rule stays `value < 1`. Error text stays `<NAME> must be a positive number, got <value>`.
- Field order is the struct order: access-token TTL, refresh TTL, password length, login attempts, login window, login block.
- Do not walk nested log values. Do not change redaction output.
- Do not touch health probes, `version`, or the `httpx` response writers. Plan `n2-health-probes` owns the probes.

## Impact Areas

- API / contract: no
- DB / migrations: no
- Auth / session: no
- Queue / jobs: no
- Config / secrets: no (startup error names one field, in a fixed order)
- Observability: no (comment only)
- CI / release: no

## Acceptance Criteria

1. `ProductionLike`'s comment names `NewKeyRing` and `NewVerifier` as the enforcers. `Validate` still does not call `ProductionLike`.
2. Two auth integers below 1 produce the error for the earlier field in the list above.
3. `redact` states that nested maps, `slog.Any` values, message text, and a camelCase key such as `accessToken` are not scanned.

## Steps

- [x] Add the `ProductionLike` sentence.
- [x] Replace the auth-integer map with an ordered slice.
- [x] Test two invalid integers and assert the first name.
- [x] Comment the `redact` limit.
- [x] `go test ./internal/platform/config ./internal/platform/logging` and `make verify`.

## Decision Log

- 2026-10-11: The stable name is a startup message change, not a client change. One bad field already failed the process. Several bad fields now always report the earliest field instead of a random map key.
- 2026-10-11: Nested redaction stays out. The comment records the hole. Walking values or refusing `KindAny` changes log output and needs its own yes.

## Verification

```bash
go test ./internal/platform/config ./internal/platform/logging -count=1
make verify
```

Outcome (2026-10-11): both commands passed. `make verify` full profile passed in 22.66s.

## Completion Notes

`ProductionLike` now says `platform/auth` `NewKeyRing` and `NewVerifier` enforce secrets and `Validate` does not. The six auth integers are an ordered slice. Two zeros report `AUTH_ACCESS_TOKEN_TTL_SECONDS must be a positive number, got 0`. `redact` documents that nested maps, `slog.Any`, message text, and `accessToken` are not scanned. Redaction output is unchanged. Health and `version` were not touched.

## Follow-Ups

- [ ] N2 health probes
- [ ] Nested redaction, after an explicit yes
- [ ] `version` comment, ldflags, or package deletion, after an explicit yes
