# configuration

One struct, `internal/platform/config`, with `env` tags and defaults; parsed once at
process start and passed down as a value. Nothing else reads the environment.

## Adding a key

1. Add the field with its tag and a one-line comment.
2. Add the same key to `env.example`, with a short comment. The knowledge check fails
   when the file and the struct drift apart: `env.example` is the deployment
   contract.
3. Validate it in `Load` — range checks and required pairs, so a bad value fails at
   startup rather than at first use.
4. Run `make verify`.

## Which key is required depends on the engine

The database key follows the compiled lane: the SQLite build requires
`DATABASE_PATH`, the pg build requires `DATABASE_URL`, and each lane's file owns the
check (`config/database_sqlite.go`, `config/database_pg.go`). A missing key is
therefore also the wrong-build guard: the pg lane never demands a value it would not
read.

## Thresholds live here, not in code

Rate limits, TTLs, presign windows, and the upload-expiry delay are configuration
with production defaults, so an operator can tune them without a rebuild. Feature
code reads them from the config it was handed; a literal threshold in a feature is a
review finding.

## Secrets

Secrets never go in git. `env.example` documents the key with a placeholder; the
value arrives at runtime. Credentials that come as JSON (an FCM service account) are
accepted raw, base64-encoded, or as a file path, and the adapter decides which.

## Fail closed

A feature whose collaborator is unconfigured must refuse, not pretend: the push
routes answer `501` when the provider is absent, the adapter reports
`push/not-configured`, and the worker does not register handlers it cannot serve.
