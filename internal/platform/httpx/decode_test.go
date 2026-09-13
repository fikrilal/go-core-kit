package httpx_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

type signupInput struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
	Age      int    `json:"age" validate:"omitempty,gte=13"`
}

func decode(t *testing.T, body string) (signupInput, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/signup", strings.NewReader(body))
	var in signupInput
	if err := httpx.Decode(req, &in); err != nil {
		return in, fmt.Errorf("decode: %w", err)
	}
	return in, nil
}

func requireIssue(t *testing.T, err error, field string) httpx.FieldIssue {
	t.Helper()
	var pe *httpx.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	if pe.Status != http.StatusBadRequest || pe.Code != httpx.CodeValidationFailed {
		t.Errorf("problem = %+v, want 400 VALIDATION_FAILED", pe)
	}
	for _, issue := range pe.Issues {
		if issue.Field == field {
			return issue
		}
	}
	t.Fatalf("no issue for field %q in %+v", field, pe.Issues)
	return httpx.FieldIssue{}
}

func assertInvalidBody(t *testing.T, err error) {
	t.Helper()
	var pe *httpx.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	if pe.Status != http.StatusBadRequest || pe.Code != httpx.CodeValidationFailed || pe.Detail != "invalid JSON body" {
		t.Errorf("problem = %+v, want invalid-body 400", pe)
	}
}

func TestDecodeValidInput(t *testing.T) {
	in, err := decode(t, `{"email":"a@b.co","password":"longenough","age":20}`)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if in.Email != "a@b.co" || in.Password != "longenough" || in.Age != 20 {
		t.Errorf("unexpected input: %+v", in)
	}
}

func TestDecodeMissingRequiredField(t *testing.T) {
	_, err := decode(t, `{"password":"longenough"}`)
	if issue := requireIssue(t, err, "email"); issue.Message != "is required" {
		t.Errorf("message = %q, want is required", issue.Message)
	}
}

func TestDecodeInvalidEmail(t *testing.T) {
	_, err := decode(t, `{"email":"nope","password":"longenough"}`)
	if issue := requireIssue(t, err, "email"); issue.Message != "must be a valid email" {
		t.Errorf("message = %q, want email message", issue.Message)
	}
}

func TestDecodeMultipleIssues(t *testing.T) {
	_, err := decode(t, `{"email":"","password":"short"}`)
	var pe *httpx.ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	fields := map[string]bool{}
	for _, issue := range pe.Issues {
		fields[issue.Field] = true
	}
	if len(pe.Issues) != 2 || !fields["email"] || !fields["password"] {
		t.Errorf("issues = %+v, want email and password", pe.Issues)
	}
}

func TestDecodeMalformedJSON(t *testing.T) {
	_, err := decode(t, `{"email":`)
	assertInvalidBody(t, err)
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.co","password":"longenough","admin":true}`)
	assertInvalidBody(t, err)
}

func TestDecodeRejectsTrailingData(t *testing.T) {
	_, err := decode(t, `{"email":"a@b.co","password":"longenough"} extra`)
	assertInvalidBody(t, err)
}

func TestDecodeResponseShapeThroughHandler(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc(testOp("POST", "/signup"), func(w http.ResponseWriter, r *http.Request) {
		var in signupInput
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.Data(w, r, in.Email)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/signup",
		strings.NewReader(`{"email":"bad","password":"short"}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var body problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "VALIDATION_FAILED" || body.Status != http.StatusBadRequest {
		t.Errorf("unexpected problem: %+v", body)
	}
	if len(body.Errors) != 2 {
		t.Errorf("issues = %+v, want 2", body.Errors)
	}
}
