package httpx

// Code is a stable, machine-readable error identifier.
type Code string

// Global error codes.
const (
	CodeValidationFailed      Code = "VALIDATION_FAILED"
	CodeUnauthorized          Code = "UNAUTHORIZED"
	CodeForbidden             Code = "FORBIDDEN"
	CodeNotFound              Code = "NOT_FOUND"
	CodeConflict              Code = "CONFLICT"
	CodeRateLimited           Code = "RATE_LIMITED"
	CodeIdempotencyInProgress Code = "IDEMPOTENCY_IN_PROGRESS"
	CodeInternal              Code = "INTERNAL"
)

// Auth error codes.
const (
	CodeAuthEmailAlreadyExists     Code = "AUTH_EMAIL_ALREADY_EXISTS"
	CodeAuthInvalidCredentials     Code = "AUTH_INVALID_CREDENTIALS" //nolint:gosec // wire error code, not a credential
	CodeAuthUserSuspended          Code = "AUTH_USER_SUSPENDED"
	CodeAuthRefreshTokenInvalid    Code = "AUTH_REFRESH_TOKEN_INVALID" //nolint:gosec // wire error code, not a credential
	CodeAuthRefreshTokenExpired    Code = "AUTH_REFRESH_TOKEN_EXPIRED" //nolint:gosec // wire error code, not a credential
	CodeAuthRefreshTokenReused     Code = "AUTH_REFRESH_TOKEN_REUSED"  //nolint:gosec // wire error code, not a credential
	CodeAuthSessionRevoked         Code = "AUTH_SESSION_REVOKED"
	CodeAuthPasswordNotSet         Code = "AUTH_PASSWORD_NOT_SET"
	CodeAuthCurrentPasswordInvalid Code = "AUTH_CURRENT_PASSWORD_INVALID" //nolint:gosec // wire error code, not a credential
	CodeAuthOidcNotConfigured      Code = "AUTH_OIDC_NOT_CONFIGURED"
	CodeAuthOidcTokenInvalid       Code = "AUTH_OIDC_TOKEN_INVALID" //nolint:gosec // wire error code, not a credential
	CodeAuthOidcEmailNotVerified   Code = "AUTH_OIDC_EMAIL_NOT_VERIFIED"
	CodeAuthOidcLinkRequired       Code = "AUTH_OIDC_LINK_REQUIRED"
	CodeAuthOidcIdentityLinked     Code = "AUTH_OIDC_IDENTITY_ALREADY_LINKED"
	CodeAuthOidcProviderLinked     Code = "AUTH_OIDC_PROVIDER_ALREADY_LINKED"
)
