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

// pushNotConfiguredError mirrors the reference's provider-configured gate.
func pushNotConfiguredError() error {
	return &httpx.ProblemError{
		Status: http.StatusNotImplemented,
		Code:   httpx.CodeAuthPushNotConfigured,
		Detail: "Push provider is not configured",
	}
}

// objectStorageNotConfiguredError mirrors the reference's storage gate.
func objectStorageNotConfiguredError() error {
	return &httpx.ProblemError{
		Status: http.StatusNotImplemented,
		Code:   httpx.CodeUsersObjectStorageNotConfigured,
		Detail: "Object storage is not configured",
	}
}

// fileNotFoundError reports an unknown or foreign stored file.
func fileNotFoundError() error {
	return &httpx.ProblemError{Status: http.StatusNotFound, Code: httpx.CodeNotFound, Detail: "File not found"}
}

// uploadNotUploadedError reports a rejected or missing upload.
func uploadNotUploadedError(detail string) error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeUsersProfileImageNotUploaded,
		Detail: detail,
	}
}

// sizeMismatchError reports an upload whose stored size differs.
func sizeMismatchError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeUsersProfileImageSizeMismatch,
		Detail: "Uploaded file size does not match the expected size",
	}
}

// contentTypeMismatchError reports an upload whose stored type differs.
func contentTypeMismatchError() error {
	return &httpx.ProblemError{
		Status: http.StatusConflict,
		Code:   httpx.CodeUsersProfileImageContentTypeMismatch,
		Detail: "Uploaded file content type does not match the expected content type",
	}
}
