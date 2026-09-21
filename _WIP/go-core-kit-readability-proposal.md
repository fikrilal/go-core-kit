# Go Core Kit Readability Proposal

- Status: Draft
- Date: 2026-10-11
- Scope: implementation taste of hand-written Go in this repository, judged against `mobile-core-kit`
- Non-scope: code changes, a whole-repo audit, wire-contract changes, a new architecture, copying Dart or Flutter

## Summary

Feature code in `mobile-core-kit` stays easy to read because the shared core already owns the repeated work. A page decides which state it is in. A repository runs one sequence and hands errors to a mapper. A use case states the policy and stops. The next feature looks like the last one.

go-core-kit already chose that shape. [ADR 0001](../docs/adr/0001-modular-monolith-and-progressive-feature-tiers.md) says the house style is a heavy shared core that keeps feature work small, with `mobile-core-kit` ADR 0009 as the source. This proposal defines how we will judge whether the Go syntax actually reaches that taste, one section at a time.

No production code changes until a section note for that section is accepted.

## The bar

The reader is a developer who can read the language syntax and still gets lost when ownership or branches hide. Junior, mid, and senior should all be able to follow a function without recovering unstated context. The author of this kit is still learning Go, so a line that depends on trivia fails the bar even when it compiles and is idiomatic among Go specialists.

Four habits from the mobile kit are the ruler. Paths below are in `/home/fikrilal/workspace/devs/core/mobile-core-kit`.

### 1. A state decision is an exhaustive switch

`AppAsyncStateView.build` switches on `AppAsyncStatus`. Each arm is one named builder: initial, loading, success, empty, failure. The compiler forces every status to appear.

`PasswordResetConfirmPage._mapStatusToViewState` does the same for page status. Token failure is decided first. The remaining statuses map in one switch. The page does not bury that decision inside nested conditionals.

Go has no widget tree. The same habit still applies wherever one value picks an outcome: HTTP status, domain error, session state, engine-specific branch. The reader sees every arm. Each arm names what happens.

### 2. The repository orchestrates. Mapping lives next to it

`AuthRepositoryImpl.login` does four things, in order: load device identity, build the request, call the remote source, convert the result. The conversion is:

```dart
return apiResponse
    .toEitherWithFallback('Login failed.')
    .mapLeft(mapAuthFailure)
    .map((m) => m.toSessionEntity());
```

`mapAuthFailure` lives in `lib/features/auth/data/error/auth_failure_mapper.dart`. The file's own comment states the rule: the repository stays orchestration-only, and the mapping is easy to test. The same comment allows a light feature to keep a tiny map inline. A separate file is for a real table, not for one line.

### 3. The use case is the policy

`LoginUserUseCase.call` builds `LoginCredentials`. Invalid input becomes `AuthFailure.validation`. Valid input calls `_repository.login`. Nothing in that function knows HTTP, JSON, logging, or device identity.

### 4. Core is why the next feature is readable

Those short functions exist because shared code already did the hard part:

- `core/infra/network` turns an API response into a typed failure.
- `core/domain/auth/auth_failure.dart` is the failure vocabulary features return.
- `core/foundation/validation` is the validation error shape.
- `AppAsyncStateView` is the page-state switch features reuse.
- Device identity, session entities, and logging are core services the repository calls.

A new feature copies the shape. It does not invent a second way to map an error or a second way to branch on status.

## What this is not

- Do not port `fpdart` `Either`, Freezed, Cubit, or widget structure into Go.
- Do not treat short Go names (`ctx`, `err`) as a defect when the function around them is still obvious.
- Do not review generated code as authorship. `internal/db/gen` and `contracts/generated.go` stay generated. Review the hand-written SQL, the callers, and the allowlist.
- Do not split a one-line mapping into a new file. Mobile's mapper comment already rejects that.
- Do not change the wire contract, error codes, token semantics, or schema as part of a readability edit. A real bug found along the way is recorded separately and fixed only when called out as a bug.
- Do not reopen ADR 0001. Platform owns cross-cutting work. Features stay on the smallest tier that fits. Use cases exist only when there is policy worth isolating.

## Where the same jobs live in Go today

| Mobile habit | Mobile home | Go home |
| --- | --- | --- |
| Shared response and failure machinery | `lib/core/infra/network` | `internal/platform/httpx` |
| Shared small primitives | `lib/core/foundation`, `lib/core/platform` | `internal/platform/config`, `logging`, `clock`, `health`, `redis`, `password`, `db` |
| Token issue and verification | session core plus feature adapter | `internal/platform/auth` |
| Thin use case | `lib/features/auth/domain/usecase` | `internal/features/auth/app` |
| Orchestration plus a side mapper | `data/repository` plus `data/error` | `internal/features/auth/infra` and `internal/features/auth/domain` |
| Process entry | `lib/core/di` | `cmd/api` |

This table names owners. It is not a score. The Go side has not been reviewed yet.

One line was read while writing this bar, so later notes do not start from zero. `Service.Register` in `internal/features/auth/app/service.go` hashes the password, creates the user, and on `domain.ErrEmailExists` builds an `httpx.ProblemError` with status, code, and detail. In the mobile kit that HTTP translation would not sit inside `LoginUserUseCase`. Whether this Go function should change is a question for the auth-feature note, not a fix authorized here.

## How the work proceeds

1. This proposal is the ruler.
2. One section note per pass, written before any edit in that section. The note cites file and function, and sorts each observation into three buckets:
   - hard to read, with the mobile habit it misses;
   - correct Go that should stay, including idioms that look odd but keep the function obvious;
   - a real bug, kept separate from taste.
3. The note also answers one design question: which repeated mechanic still sits in the feature and should move into platform, and which policy must stay in the feature so the function still says what the product does.
4. An accepted note is the only permission to edit that section. The edit stays inside the note. Behavior stays unless the note marked a bug.
5. Verify with the tests that cover the section. Record the command and the result on the note.
6. The next section starts only after that. No batched cleanup across unreviewed packages.

Section notes live in `_WIP/readability/`. They are investigation notes, same status as this folder: not source of truth. A rule that survives several sections and should bind future features moves into `docs/standards/` in its own change, after the evidence exists.

## Section order

Order follows the reason the mobile features are readable. Judge the shared core first. Judge a feature only after the core it should be calling has a note.

1. `internal/platform/httpx` — envelope, problem details, decode, middleware, router, idempotency, list query. This is the machinery every later handler should call instead of reimplementing.
2. `internal/platform/config`, `logging`, `clock`, `health`, `version` — small primitives. One note covers the set.
3. `internal/platform/auth` — issue, verify, guard.
4. `internal/platform/db`, hand-written `db/queries`, hand-written `db/migrations`. Generated `internal/db/gen` is out of the review.
5. `internal/platform/redis`, `internal/platform/password`.
6. `internal/features/auth` — the only vertical slice, and the template later features will copy. Judge `app` against habit 3, `infra` against habit 2, and domain errors against habit 1.
7. `cmd/api` — composition root (`main.go`, `server.go`). Contract tests in that folder are a separate note if their readability blocks understanding the boot path.
8. `contracts` allowlist and `tools/gokit` — different reader, last.

Packages named in the architecture doc and absent on disk (`cmd/worker`, `rbac`, `queue`, `otel`, `internal/shared`) wait until they exist.

A section that is too large to read in one sitting splits on the first note, by file group, before any edit. `httpx` is the likely candidate.

## Acceptance

The program is done when all of the following hold for the hand-written surface above:

- A reader who has learned one feature can predict the shape of the next feature.
- A feature function shows policy. Platform code shows the repeated mechanic.
- Each boundary has one obvious place that translates errors. Callers of that translation read as a straight sequence.
- Outcome branches the caller must handle are exhaustive and named.
- Readability edits do not move the wire contract, the schema, or auth semantics.

## Risks

- A taste pass becomes a rewrite that fights ADR 0001 or ordinary Go.
- Every mapping gains a file, including maps mobile would have left inline.
- Platform grows a helper that only one call site uses. That is a speculative abstraction. The note must show a second call site or a feature body that is hard to read without it.
- A readability edit changes status codes, error codes, or token behavior without a separate bug note.

## Open questions

- Section notes: write each one, stop for acceptance, then edit. Recommended default. Alternative is to edit inside the same pass once the note's findings are listed. Waiting is the default until this draft is accepted with the other choice.
- After `httpx` and `features/auth` both have accepted notes, decide whether the surviving rules belong in `docs/standards/`. Not before that evidence exists.
