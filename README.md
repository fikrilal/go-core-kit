# Go Core Kit

Production-grade Go backend template — the Go member of the core-kit family. It serves the same wire contract as `backend-core-kit` (envelope, RFC7807 problem details, stable error codes, first-party session tokens), so existing frontend-core-kit and mobile-core-kit clients need no tuning.

- **SQLite first** with an opt-in Postgres build (`-tags pg`); no database server by default.
- Stdlib HTTP, `sqlc`, `asynq`, `log/slog` — no ORM, no reflection, no DI container.
- API + worker processes from one module; static `CGO_ENABLED=0` binaries.

Status: foundation complete; platform core in progress. Roadmap: `docs/exec-plans/roadmap.md`. Decisions are recorded in `docs/adr/`; proposals live in `_WIP/`.

## Requirements

- Go — pinned by `go.mod` (`go 1.27.2`)
- GNU Make
- `golangci-lint` v2.14.0 — run `make tools` (installs the pinned version), or:
  `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b ~/.local/bin v2.14.0`

## Commands

- `make verify` — format check, vet, lint (includes boundary rules), tests, build
- `make fmt` — format the tree
- `make test` / `make build`

## Pointers

- Docs index: `docs/README.md`
- Architecture: `docs/core/project-architecture.md`
- Stack: `docs/core/project-stack.md`
- ADRs: `docs/adr/README.md`
