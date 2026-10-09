package app

import (
	"net/http"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

// unauthorizedError masks a missing or deleted principal as an unusable token.
func unauthorizedError() error {
	return &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized}
}

// profilePatchRequiredError reports a patch that defined no profile field.
func profilePatchRequiredError() error {
	return &httpx.ProblemError{
		Status: http.StatusBadRequest,
		Code:   httpx.CodeValidationFailed,
		Issues: []httpx.FieldIssue{{Field: "profile", Message: "At least one profile field must be provided"}},
	}
}

// validationFailedError reports the collected field issues.
func validationFailedError(issues []httpx.FieldIssue) error {
	return &httpx.ProblemError{Status: http.StatusBadRequest, Code: httpx.CodeValidationFailed, Issues: issues}
}

// sessionNotFoundError reports an unknown or foreign session.
func sessionNotFoundError() error {
	return &httpx.ProblemError{Status: http.StatusNotFound, Code: httpx.CodeNotFound, Detail: "Session not found"}
}
