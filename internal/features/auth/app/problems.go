package app

import (
	"fmt"
	"net/http"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

func emailAlreadyExistsError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeAuthEmailAlreadyExists,
		Detail: "Email already exists",
	}
}

func newPasswordMustDifferError() error {
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeValidationFailed,
		Issues: []httpx.FieldIssue{{Field: "newPassword", Message: "New password must be different"}},
	}
}

func passwordTooShortError(field string, minLength int) error {
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeValidationFailed,
		Issues: []httpx.FieldIssue{{
			Field:   field,
			Message: fmt.Sprintf("Password must be at least %d characters", minLength),
		}},
	}
}

func invalidCredentials() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthInvalidCredentials,
		Detail: "Invalid credentials",
	}
}

func invalidRefreshToken() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthRefreshTokenInvalid,
		Detail: "Invalid refresh token",
	}
}

func refreshTokenExpiredError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthRefreshTokenExpired,
		Detail: "Refresh token expired",
	}
}

func refreshTokenReusedError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthRefreshTokenReused,
		Detail: "Refresh token reuse detected",
	}
}

func sessionRevokedError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthSessionRevoked,
		Detail: "Session revoked",
	}
}

func suspendedError() error {
	return &httpx.ProblemError{
		Status: http.StatusForbidden,
		Code:   httpx.CodeAuthUserSuspended,
		Detail: "User is suspended",
	}
}

// emailVerificationTokenInvalidError hides whether the token ever existed.
func emailVerificationTokenInvalidError() error {
	return &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeAuthEmailVerificationTokenInvalid}
}

// emailVerificationTokenExpiredError reports a token that was used or expired.
func emailVerificationTokenExpiredError() error {
	return &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeAuthEmailVerificationTokenExpired}
}

// verificationEmailNotConfiguredError reports the provider gate.
func verificationEmailNotConfiguredError() error {
	return &httpx.ProblemError{Status: http.StatusNotImplemented, Code: httpx.CodeAuthEmailNotConfigured}
}

// passwordResetTokenInvalidError covers an unknown token and an unverifiable
// account, deliberately as one answer.
func passwordResetTokenInvalidError() error {
	return &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeAuthPasswordResetTokenInvalid}
}

// passwordResetTokenExpiredError reports a token that was used or expired.
func passwordResetTokenExpiredError() error {
	return &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeAuthPasswordResetTokenExpired}
}

func unauthorizedError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeUnauthorized,
	}
}

func passwordNotSetError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeAuthPasswordNotSet,
		Detail: "Password is not set for this account",
	}
}

func currentPasswordInvalidError() error {
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeAuthCurrentPasswordInvalid,
		Detail: "Current password is invalid",
	}
}

func oidcNotConfiguredError() error {
	return &httpx.ProblemError{
		Status: http.StatusInternalServerError,
		Code:   httpx.CodeAuthOidcNotConfigured,
		Detail: "OIDC is not configured",
	}
}

func oidcTokenInvalidError() error {
	return &httpx.ProblemError{
		Status: http.StatusUnauthorized,
		Code:   httpx.CodeAuthOidcTokenInvalid,
		Detail: "Invalid OIDC token",
	}
}

func oidcEmailNotVerifiedError() error {
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeAuthOidcEmailNotVerified,
		Detail: "Email is not verified",
	}
}

func oidcLinkRequiredError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeAuthOidcLinkRequired,
		Detail: "We found an existing account for this email. Sign in with your password to link Google sign-in.",
	}
}

func oidcIdentityAlreadyLinkedError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeAuthOidcIdentityLinked,
		Detail: "This OIDC identity is already linked to another account",
	}
}

func oidcProviderAlreadyLinkedError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeAuthOidcProviderLinked,
		Detail: "This provider is already linked to your account",
	}
}
