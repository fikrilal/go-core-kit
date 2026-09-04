# Project Stack

Decided 2026-10-10 and recorded per-topic in `docs/adr/`. Changing a decision requires a superseding ADR.

- **Language**: Go, current stable; pinned by `go.mod` (`go 1.27.2`), toolchain directive enforced in CI.
- **HTTP**: stdlib `net/http` (method + wildcard patterns) with `internal/platform/httpx` middleware and response helpers. See ADR-0003.
- **Data layer**: SQLite first (`modernc.org/sqlite`, pure Go); opt-in Postgres build (`-tags pg`, `pgx/v5`). See ADR-0004.
- **DB access**: `sqlc` with the portable SQL subset; `goose` per-engine migrations; generated code committed and drift-gated. See ADR-0005.
- **Cache / queue**: Redis (`go-redis/v9`) — `asynq` queue, idempotency, rate limits. See ADR-0006.
- **Auth**: `lestrrat-go/jwx/v3` — multi-`kid` keyring, EdDSA default, RS256 supported, JWKS endpoint; opaque refresh tokens with rotation and reuse detection; `x/crypto/argon2` (argon2id, PHC format) for passwords.
- **Validation**: `go-playground/validator/v10` behind a small `httpx` wrapper producing `errors[] {field, message}` with `VALIDATION_FAILED`.
- **Config**: `caarlos0/env` + explicit `Validate()` invariants. See ADR-0007.
- **Logging**: `log/slog` JSON handler + redaction. See ADR-0009.
- **Tracing**: OpenTelemetry SDK + OTLP HTTP exporter; disabled cleanly when env is absent.
- **Storage**: `aws-sdk-go-v2` S3 client (R2/S3-compatible) for presigning.
- **Email**: thin internal HTTP adapter (Resend API), fenced behind a port.
- **Push**: FCM HTTP v1 with an `x/oauth2/google` token source.
- **Contract types**: `oapi-codegen` (types only) from the pinned snapshot. See ADR-0002.
- **Contract tests**: `kin-openapi` response validation + parity assertions.
- **Spec lint**: `vacuum` (Go-native).
- **Boundaries**: `golangci-lint` + `depguard`, pinned v2.14.0. See ADR-0008.
- **CLI**: `tools/gokit` (scaffold / remove / db generate / verify / doctor). See ADR-0010.
- **Containers**: multi-stage build → distroless, static binary, `CGO_ENABLED=0`.

Alternatives considered for each slot are recorded in the ADRs and in the `_WIP/` proposals.
