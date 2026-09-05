package httpx_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

type problemResponse struct {
	Type    string             `json:"type"`
	Title   string             `json:"title"`
	Status  int                `json:"status"`
	Detail  string             `json:"detail"`
	Code    string             `json:"code"`
	TraceID string             `json:"traceId"`
	Errors  []httpx.FieldIssue `json:"errors"`
}

func TestWriteErrorRendersProblemDetails(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, &httpx.ProblemError{
			Status: http.StatusUnauthorized,
			Code:   httpx.CodeUnauthorized,
			Detail: "invalid credentials",
			Issues: []httpx.FieldIssue{{Field: "email", Message: "must not be empty"}},
		})
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/boom", nil)
	req.Header.Set("X-Request-Id", "req-123")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Type != "about:blank" || body.Title != "Unauthorized" || body.Status != http.StatusUnauthorized {
		t.Errorf("unexpected problem core: %+v", body)
	}
	if body.Code != "UNAUTHORIZED" || body.TraceID != "req-123" || body.Detail != "invalid credentials" {
		t.Errorf("unexpected problem fields: %+v", body)
	}
	if len(body.Errors) != 1 || body.Errors[0].Field != "email" {
		t.Errorf("unexpected issues: %+v", body.Errors)
	}
}

func TestWriteErrorSetsRetryAfter(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("POST /limited", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, &httpx.ProblemError{
			Status:            http.StatusTooManyRequests,
			Code:              httpx.CodeRateLimited,
			RetryAfterSeconds: 30,
		})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/limited", nil))

	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
}

func TestWriteErrorUnknownIsInternalAndLogged(t *testing.T) {
	router, logs := testRouter()
	router.HandleFunc("GET /explode", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, errors.New("db exploded"))
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/explode", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "db exploded") {
		t.Errorf("response leaked internal error: %s", rec.Body.String())
	}
	var body problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "INTERNAL" || body.Detail != "" {
		t.Errorf("unexpected problem: %+v", body)
	}
	if !strings.Contains(logs.String(), "unhandled request error") || !strings.Contains(logs.String(), "db exploded") {
		t.Errorf("internal error not logged: %s", logs.String())
	}
}
