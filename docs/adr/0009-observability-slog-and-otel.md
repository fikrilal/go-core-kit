# ADR: Observability — slog and OpenTelemetry

- Status: Accepted
- Date: 2026-10-10
- Decision makers: Core kit maintainers

## Context

The contract includes `traceId` on every error (equal to `X-Request-Id`) and an optional `otelTraceId` when tracing is enabled. The kit needs structured logs, request correlation, and optional tracing without a heavy dependency surface.

## Decision

- **Logging**: stdlib `log/slog` JSON handler; request ID and trace ID bound per request; an explicit redaction list; health/ready responses excluded from request-log noise.
- **Tracing**: OpenTelemetry SDK with the OTLP HTTP exporter; disabled cleanly when env configuration is absent.
- **Correlation**: the request-ID middleware owns acceptance, generation, and echo of `X-Request-Id`; the problem mapper reads it for `traceId` and adds `otelTraceId` when a span is active.

## Rationale

Stdlib logging keeps dependencies minimal; OTel is optional infrastructure that integrates with the family's observability baseline; correlation is one mechanism owned once in the platform.

## Consequences

Positive: no logger dependency; tracing is opt-in per environment; contract fields are populated correctly.

Negative: redaction and field conventions must be maintained as logging grows.

## Alternatives Considered

- zap / zerolog — rejected: third-party surface without a demonstrated need.
- No tracing — rejected: the contract supports `otelTraceId`, and runtime evidence benefits from spans.

## Links / References

- Related docs: `docs/core/project-architecture.md`
- Related proposals: `_WIP/go-core-kit-architecture-proposal.md`
