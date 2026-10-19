# duplication

Duplication is a signal, not a verdict. The linter runs `dupl` at threshold 100 in
the lint step (`make lint`, and therefore both verify profiles), so a copy long
enough to be worth one name fails the gate. The rule exists because repetition of a
*decision* drifts: two copies of a policy are two places to change and one to
forget.

## What the gate catches

`golangci-lint run` fails when a function is a token-for-token copy of another one
past the threshold. The threshold is deliberately high: it targets sequences long
enough to carry a decision — a loader, a mapping, a validation pass — and ignores
the incidental symmetry of two short functions that happen to start alike.

## What is not duplication

Two pieces of code can look alike and be genuinely different:

- **Two ports over different generated row types.** `EmailVerificationTokenByHash`
  and `PasswordResetTokenByHash` differed only in their query and their row type.
  The fix was not a suppression and not a copy: it was one method with a kind
  discriminator (`TokenByHash(kind, hash)`), because the *decision* — "how a token
  lookup behaves" — is single.
- **Two adapters over different providers.** The login and resend limiters share a
  shape because they share a policy (attempts, window, block); they are one
  implementation with per-route scope and message, not two.
- **Fixtures that mirror a table.** Two seeding helpers for two tables differ in
  what they insert, which is the point; if they collapse, they collapse into a
  helper parameterized by the table, as the contract tests' `seedAuthToken` does.

The test is always the same: is there one decision here, or two that currently agree?
One decision means one implementation. Two decisions that merely look alike are fine
and the linter will not flag them at this threshold.

## Requesting an exception

An exception is a reviewed decision, not a comment:

1. Add `//nolint:dupl // <reason>` with a one-line reason that names the distinct
   decisions.
2. Add a row to the table below with the path and the same reason.
3. A reviewer checks that the reason describes two decisions rather than one
   avoided.

## Reviewed exceptions

| Path | Reason |
| --- | --- |
| — | none today: the token loaders merged into `TokenByHash`, the limiters share one implementation, and the contract-test seeders collapsed into `seedAuthToken`. |

An empty table is the healthy state. If it grows, the rule is being absorbed rather
than applied.

## Prove the gate

`gokit gates honesty` includes a scenario that writes two identical functions into a
temporary package inside the module and asserts the lint gate fails with the dupl
marker. A rule that cannot fail is not a rule (ADR-0011).
