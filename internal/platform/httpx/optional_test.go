package httpx_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

type optionalInput struct {
	DisplayName httpx.OptionalString `json:"displayName"`
	GivenName   httpx.OptionalString `json:"givenName"`
}

func decodeOptional(t *testing.T, body string) (optionalInput, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/patch", strings.NewReader(body))
	var in optionalInput
	if err := httpx.Decode(req, &in); err != nil {
		return in, fmt.Errorf("decode: %w", err)
	}
	return in, nil
}

func TestOptionalStringAbsent(t *testing.T) {
	in, err := decodeOptional(t, `{}`)
	if err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if in.DisplayName.Present() || in.DisplayName.Null() || in.DisplayName.Value() != "" {
		t.Errorf("absent field = present %t null %t value %q, want all zero",
			in.DisplayName.Present(), in.DisplayName.Null(), in.DisplayName.Value())
	}
}

func TestOptionalStringNull(t *testing.T) {
	in, err := decodeOptional(t, `{"displayName":null}`)
	if err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if !in.DisplayName.Present() || !in.DisplayName.Null() || in.DisplayName.Value() != "" {
		t.Errorf("null field = present %t null %t value %q, want present null",
			in.DisplayName.Present(), in.DisplayName.Null(), in.DisplayName.Value())
	}
}

func TestOptionalStringValueIsTrimmed(t *testing.T) {
	in, err := decodeOptional(t, `{"displayName":"  Dante  "}`)
	if err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if !in.DisplayName.Present() || in.DisplayName.Null() || in.DisplayName.Value() != "Dante" {
		t.Errorf("value field = present %t null %t value %q, want trimmed Dante",
			in.DisplayName.Present(), in.DisplayName.Null(), in.DisplayName.Value())
	}
	if in.GivenName.Present() {
		t.Error("givenName = present, want absent")
	}
}

func TestOptionalStringBlankArrivesEmpty(t *testing.T) {
	in, err := decodeOptional(t, `{"displayName":"   "}`)
	if err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if !in.DisplayName.Present() || in.DisplayName.Null() || in.DisplayName.Value() != "" {
		t.Errorf("blank field = present %t null %t value %q, want present empty",
			in.DisplayName.Present(), in.DisplayName.Null(), in.DisplayName.Value())
	}
}

func TestOptionalStringWrongTypeIsInvalidBody(t *testing.T) {
	_, err := decodeOptional(t, `{"displayName":5}`)
	var problem *httpx.ProblemError
	if !errors.As(err, &problem) {
		t.Fatalf("error = %v, want ProblemError", err)
	}
	if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
		t.Errorf("problem = %+v, want 400 VALIDATION_FAILED", problem)
	}
}
