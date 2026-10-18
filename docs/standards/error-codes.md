# error-codes

Every failure a client can act on has a stable `code`. Codes are contract surface:
they are declared by the pinned snapshot's `x-error-codes`, and clients branch on
them.

## Definitions

Codes are typed constants in `internal/platform/httpx/codes.go`, grouped by family:
globals (`VALIDATION_FAILED`, `UNAUTHORIZED`, `FORBIDDEN`, `NOT_FOUND`, `CONFLICT`,
`RATE_LIMITED`, `IDEMPOTENCY_IN_PROGRESS`, `INTERNAL`), auth codes, and users codes.
A raw production error-code string is rejected by construction — there is no path
from a literal to a response.

## Shape

Problems are RFC7807 (`docs/standards/api-response.md`): the same code always
carries the same HTTP status, and the status is chosen for the family rather than
the endpoint — authentication-family failures answer `401`, a state that forbids
the action answers `409`, a missing object `404`, a limiter `429` with
`Retry-After`, and anything unexpected `500 INTERNAL` with no detail leaked.

## Codes that hide a distinction on purpose

Some codes cover two situations because telling them apart would leak state:

- `AUTH_EMAIL_VERIFICATION_TOKEN_INVALID` covers an unknown token *and* an account
  that cannot be verified.
- `AUTH_PASSWORD_RESET_TOKEN_INVALID` does the same for resets.
- Password-reset requests and verification resends answer `204` for an unknown
  address; nothing is enqueued, and the response is identical.

The "expired" codes are separate on purpose: a used token and a past-expiry token
are both retries the client should not repeat, and a race the request lost is
reported the same way.

## Adding one

Add the constant to `httpx`, name the problem helper in the feature's
`app/problems.go`, and — if the snapshot declares it — the operation's
`x-error-codes` list must already contain it. Features never build a problem
inline; the helper is the one place a status and a code meet.
