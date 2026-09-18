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
	CodeAuthEmailAlreadyExists  Code = "AUTH_EMAIL_ALREADY_EXISTS"
	CodeAuthInvalidCredentials  Code = "AUTH_INVALID_CREDENTIALS" //nolint:gosec // wire error code, not a credential
	CodeAuthUserSuspended       Code = "AUTH_USER_SUSPENDED"
	CodeAuthRefreshTokenInvalid Code = "AUTH_REFRESH_TOKEN_INVALID" //nolint:gosec // wire error code, not a credential
	CodeAuthRefreshTokenExpired Code = "AUTH_REFRESH_TOKEN_EXPIRED" //nolint:gosec // wire error code, not a credential
	CodeAuthRefreshTokenReused  Code = "AUTH_REFRESH_TOKEN_REUSED"  //nolint:gosec // wire error code, not a credential
	CodeAuthSessionRevoked      Code = "AUTH_SESSION_REVOKED"
)
