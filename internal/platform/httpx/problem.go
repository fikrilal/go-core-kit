package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

// FieldIssue describes a validation problem for a single request field.
type FieldIssue struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ProblemError is an expected request failure rendered as RFC7807 problem details.
type ProblemError struct {
	Status            int
	Code              Code
	Detail            string
	Issues            []FieldIssue
	RetryAfterSeconds int
}

// Error returns the detail when set, otherwise the code.
func (e *ProblemError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return string(e.Code)
}

type problem struct {
	Type    string       `json:"type"`
	Title   string       `json:"title"`
	Status  int          `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Code    Code         `json:"code"`
	TraceID string       `json:"traceId,omitempty"`
	Errors  []FieldIssue `json:"errors,omitempty"`
}

// WriteError renders err as an RFC7807 problem response.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var pe *ProblemError
	if !errors.As(err, &pe) {
		Logger(r).Error("unhandled request error", slog.Any("error", err))
		pe = &ProblemError{Status: http.StatusInternalServerError, Code: CodeInternal}
	}
	writeProblem(w, r, pe)
}

func writeProblem(w http.ResponseWriter, r *http.Request, pe *ProblemError) {
	if pe.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(pe.RetryAfterSeconds))
	}
	title := http.StatusText(pe.Status)
	if title == "" {
		title = "Error"
	}
	body, _ := json.Marshal(problem{
		Type:    "about:blank",
		Title:   title,
		Status:  pe.Status,
		Detail:  pe.Detail,
		Code:    pe.Code,
		TraceID: RequestID(r),
		Errors:  pe.Issues,
	})
	writeBody(w, pe.Status, contentTypeProblem, body)
}
