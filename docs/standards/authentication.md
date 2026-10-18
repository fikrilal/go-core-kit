# authentication

## Tokens

Access tokens are JWTs issued by the auth feature: asymmetric keys in a keyring,
`kid` in the header, and a JWKS at `/.well-known/jwks.json`. Refresh tokens are
opaque, stored hashed, and rotated on every use; a refresh token presented twice
means the pair leaked, so the session is revoked rather than reissued.

Sessions are per device: logging in from the same device id revokes the session it
replaces, so a device holds one live session. Logout revokes the session and its
refresh tokens.

## Per-request checks

The guard (`platform/auth`) verifies the signature, expiry, and audience, then
loads the session: a revoked or expired session, or an account that is no longer
active, fails as `401 UNAUTHORIZED`. Handlers read the principal from the request
context; they never re-parse the token.

## Account states

- `ACTIVE` — normal.
- `SUSPENDED` — authentication fails (`AUTH_USER_SUSPENDED`); an OIDC connect is
  still allowed, because linking an identity is not access.
- `DELETED` — every flow except the deletion endpoints treats the account as gone;
  reads answer `401`, and the finalize job owns the de-identification.

## Email verification and password reset

Both are token flows whose tokens are minted by the **worker**, never by the API:
a token cannot exist without the email that carries it. Verification consumes its
token and stamps `email_verified_at` in one transaction; a reset consumes its
token, replaces the credential (creating one for an account that had none), and
deletes the account's sessions — which cascade to refresh tokens — in the same
transaction. Replays answer the expired code, and unknown or unverifiable tokens
answer the invalid code.

## Rate limiting

Login, verification resend, and password reset requests are limited per key
(email, account, or claimed address) with their own Redis scope and their own 429
message. Limits are configuration (`docs/standards/configuration.md`), not
constants.

## OIDC

`auth.oidc.exchange` verifies an upstream ID token against the configured issuer's
JWKS, then either signs the user in, or asks for a link when the identity is new.
`auth.oidc.connect` links an identity to the signed-in account: already-linked to
another account answers `AUTH_OIDC_IDENTITY_ALREADY_LINKED`, a second subject for
the same provider answers `AUTH_OIDC_PROVIDER_ALREADY_LINKED`, and an upstream
email that is unverified answers `AUTH_OIDC_EMAIL_NOT_VERIFIED`.
