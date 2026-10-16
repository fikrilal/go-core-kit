# Adding a feature

A feature is a vertical slice under `internal/features/<name>/`: `domain` (core
types and rules), `ports` (boundaries), `infra` (persistence over the generated
queries), `app` (use cases), and the feature root, which owns the HTTP surface
and the wiring the composition root calls.

## Scaffold it

```bash
go run ./tools/gokit scaffold feature --name reports
```

That writes the skeleton and registers it in `cmd/api/server.go` between the
`// gokit:feature-registrations` markers. The feature mounts no routes yet, so
the contract surface does not move: no allowlist entry, no env key, no map
change.

Then `make verify`. The scaffolded skeleton is expected to pass untouched — if it
does not, that is a scaffold bug, and `make scaffold-smoke` reproduces it in a
throwaway copy.

## Where things go

- SQL belongs to the repository and `db/queries/common/<feature>.sql`; handlers
  and services never build queries.
- Time comes from the injected clock (`clock.Clock`), never `time.Now()`.
- Responses go through `httpx` helpers (`httpx.Data`, `httpx.Created`,
  `httpx.WriteError`); `encoding/json` response writes are forbidden inside
  `internal/features/**`.
- Errors that reach a client are named problems built in `app/problems.go`; the
  domain stays HTTP-free.

These are lint-enforced boundary rules: the feature lane may not import database
drivers, `encoding/json`, or `time.Now`, and `internal/platform/*` may not import
`internal/features/*`.

## Dependency rules

Features may not import each other directly. A feature exposes its `Deps` and
`Register` at its root, and cross-feature use goes through that surface only,
allowlisted per pair — `auth.Register` returning its guard for `me` to reuse is
the worked example.

## Removing a feature

```bash
go run ./tools/gokit remove feature --name reports
```

Removal unwires the composition root, returns the feature's operations to the
contract allowlist (removing an implementation un-implements them), and deletes
the feature directory plus the per-feature contract tests and query file. Text
edits happen before any deletion, so a refused removal leaves every file in
place. Run `make generate` afterwards for the removed query.
