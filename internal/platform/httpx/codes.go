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
