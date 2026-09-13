# Go Core Kit

Production-grade Go backend template — the Go member of the core-kit family. It serves the same wire contract as `backend-core-kit` (envelope, RFC7807 problem details, stable error codes, first-party session tokens), so existing frontend-core-kit and mobile-core-kit clients need no tuning.

- **SQLite first** with an opt-in Postgres build (`-tags pg`); no database server by default.
- Stdlib HTTP, `sqlc`, `asynq`, `log/slog` — no ORM, no reflection, no DI container.
- API + worker processes from one module; static `CGO_ENABLED=0` binaries.

Status: foundation complete; platform core in progress. Roadmap: `docs/exec-plans/roadmap.md`. Decisions are recorded in `docs/adr/`; proposals live in `_WIP/`.

## Requirements

- Go — pinned by `go.mod` (`go 1.27.2`)
- GNU Make
- `golangci-lint` v2.14.0 and `sqlc` v1.31.1 — run `make tools` (installs the pinned versions)

## Commands

- `make verify` — full gate: knowledge + contract checks, format, vet, lint, tests, builds, generated-code drift, spec lint, gate honesty (`gokit verify --profile full`)
- `go run ./tools/gokit verify --profile fast` — quick loop (knowledge, contract, format, vet, lint, tests)
- `go run ./tools/gokit doctor` — inspect local prerequisites
- `make generate` — regenerate sqlc output into `internal/db/gen`
- `make drift` — regenerate and fail when generated artifacts are uncommitted or stale
- `make fmt` — format the tree
- `make test` / `make build`

## Pointers

- Docs index: `docs/README.md`
- Architecture: `docs/core/project-architecture.md`
- Stack: `docs/core/project-stack.md`
- ADRs: `docs/adr/README.md`
