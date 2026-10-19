# x4 — Container variant

## Objective

Ship the deployment the template should offer beside the bare-binary path: one
Compose project per application on a shared host, with the kit's SQLite lane.

## Deliverables

- `Dockerfile` — one build stage, two targets (`api`, `worker`) on
  `distroless/static:nonroot`, arm64-buildable.
- `compose.yaml` — redis (non-persistent by intent), api (the only published port),
  worker (`depends_on: api`, since the API migrates), and litestream behind a `backup`
  profile because it needs a replica configured.
- `litestream.yml` — replica settings from the environment, documented there rather
  than in `env.example` (deployment keys, not application config).
- A "container variant" section in `docs/engineering/single-box-deployment.md`,
  including the bind-mount ownership step and the shared-host guidance.

## Acceptance checks

1. `docker compose up -d --build` on a clean checkout with **no `.env` and no
   pre-created `./data`** boots the stack — the init service creates and hands over
   the mount — and `/health` and `/ready` answer 200 through the published port.
2. The stack runs as `APP_ENV=dev` by default, so it evaluates without production
   signing keys; the doc says how to move it to `prod`.
3. `docker compose --profile backup up -d` starts Litestream only when its replica
   variables are set.
4. Images build for `linux/arm64` with `buildx --platform`, or on the host.
5. `make verify` stays green; the deployment note and README point at the variant.

## Outcome — landed 2026-10-19

`Dockerfile` (one stage, `api` and `worker` targets on `distroless/static:nonroot`,
measured at 44.5 MB and 43.9 MB), `compose.yaml`, `litestream.yml` (behind the `backup`
profile, its `LITESTREAM_*` keys documented there because they are deployment settings
rather than application config), and `.dockerignore`.

The review round changed two things worth recording, because both were my claims being
wrong rather than code being absent:

- **The out-of-box boot was not out of the box.** My first verification pre-created
  `./data` with the right owner, which the acceptance check did not say. A clean
  checkout failed with `permission denied` on a root-owned bind mount. An **init
  service** now creates and hands over the directory, so the check is true as written:
  verified by running `docker compose up -d --build` with **no `.env` and no `data`
  directory**, then `/health` and `/ready` answering 200 and the mount owned by
  65532.
- **The default `APP_ENV` was `prod`**, which crash-loops without production signing
  keys. The compose default is now `dev`; the doc says to copy `env.example` to `.env`
  and set `APP_ENV=prod` plus the keys before exposing the stack.

The import counter was also retry-unsafe — `inserted` accumulated across serializable
retries on the pg lane — and is now assigned from the attempt that commits.
`heldTables` lost its dead parameter and its comment describes what it checks, and
`data/` is ignored.

Verification: `make verify` full profile passed, `make verify-pg` passed, the db-move
tests pass on both lanes, and the container boot above.
