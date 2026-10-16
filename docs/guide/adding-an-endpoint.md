# Adding an endpoint

The wire contract is owned by backend-core-kit. This kit follows it: an endpoint
can only be added for an operation the pinned snapshot (`contracts/`) already
declares, and only while that operation is listed in `contracts/allowlist.yaml`
as unimplemented.

## Scaffold it

```bash
go run ./tools/gokit scaffold endpoint --feature reports --operation reports.list
```

The command resolves method, path, and schemas from the pinned snapshot, then:

- writes the use case (`app/`), its test, the repository read (`infra/`), and the
  query in `db/queries/common/<feature>.sql`;
- adds the handler, the route, and the handler set to the feature root, inside
  the `// gokit:route-registrations` marker, using the snapshot's operation id as
  the route id;
- writes a contract-test skeleton under `cmd/api/`;
- removes the operation from the allowlist, which is what keeps the contract
  coverage test passing instead of drifting.

Then `make generate` (for the new query) and `make verify`.

## Fill in the body

The generator leaves `ports.Repository` as the feature's own boundary and does not
add a method to it: declare the repository method the use case needs (and its row
type) yourself, so the port stays the feature's deliberate shape rather than a
generated one.

The generated use case, repository read, and contract test carry `TODO`s naming
what the snapshot declares: read the stored rows and return the response fields.
Keep the response envelope and the error mapping in `httpx` — the generator emits
`httpx.Data` and `httpx.WriteError` already.

## Conventions an endpoint must follow

- Routes are guarded: `deps.Guard.Require(http.HandlerFunc(...))`.
- Writes that retry take idempotency: `router.Idempotent(operation, handler)`, or
  `router.IdempotentFor(operation, ttl, handler)` when the operation needs its own
  window (profile-image upload is the worked example).
- Route ids are snapshot operation ids verbatim; the coverage test compares them
  against the allowlist and the snapshot in both directions.
- A `t.Skip` contract skeleton is a promise, not coverage: fill it in before
  calling the endpoint done.

## Prove the scaffold itself

`make scaffold-smoke` copies the tree, scaffolds a feature plus one allowlisted
operation, regenerates, and runs the full `make verify` profile there. It runs in
CI, so a template regression is caught before it reaches anyone.
