# Configuration

Configuration lives in `internal/platform/config`: one struct, `env` tags with
defaults, parsed once at process start and passed down as a value. Nothing else
reads the environment.

## Adding a knob

1. Add the field with its tag:

   ```go
   // ReportsRetentionDays is how long a report stays readable.
   ReportsRetentionDays int64 `env:"REPORTS_RETENTION_DAYS" envDefault:"30"`
   ```

2. Add the same key to `env.example`, with a short comment. The knowledge check
   fails when the file and the struct drift apart, which is the point: env.example
   is the deployment contract.
3. Validate it in `config.Load` (range checks, required pairs) so a bad value
   fails at startup rather than at first use. Cross-field rules live there too —
   the push lane's credential pair, for example.
4. Run `make verify`.

## What the gate checks

- `gokit knowledge check` — env.example keys against the struct.
- `gokit doctor` — `go.mod`'s version against the installed toolchain, the
  Makefile's tool pins, and that you are in a git work tree.
- `gokit drift check` — generated artifacts against what the generators produce.

## Secrets

Secrets never go in git. `env.example` documents the key with a placeholder value;
the real value is injected at runtime. Credentials that arrive as JSON (the FCM
service account) are accepted raw, base64-encoded, or as a file path, and the
adapter decides which.

## Runtime behavior

A feature that needs an unconfigured collaborator must fail closed, not panic:
the push lane's routes answer `501` when the provider is absent, its adapter
reports `push/not-configured`, and the worker simply does not register handlers it
cannot serve. Configuration decides what exists; nothing else pretends otherwise.
