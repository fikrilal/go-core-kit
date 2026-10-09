package httpx_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

func TestCursorRoundTrip(t *testing.T) {
	raw := httpx.EncodeCursor("-createdAt", map[string]string{"createdAt": "2026-10-12T12:00:00.000000000Z", "id": "session-1"})
	cursor, err := httpx.DecodeCursor(raw, "-createdAt", []string{"createdAt", "id"})
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if cursor.Sort != "-createdAt" {
		t.Errorf("sort = %q, want -createdAt", cursor.Sort)
	}
	if cursor.After["id"] != "session-1" || cursor.After["createdAt"] == "" {
		t.Errorf("after = %+v, want the encoded values", cursor.After)
	}
}

func TestCursorRejectsMissingField(t *testing.T) {
	raw := httpx.EncodeCursor("id", map[string]string{"id": "session-1"})
	if _, err := httpx.DecodeCursor(raw, "id", []string{"createdAt", "id"}); err == nil {
		t.Fatal("DecodeCursor() = nil for a cursor missing a sort field, want an error")
	}
}

func TestCursorRejectsBadInput(t *testing.T) {
	valid := httpx.EncodeCursor("id", map[string]string{"id": "session-1"})
	cases := []struct {
		name   string
		raw    string
		sort   string
		reason string
	}{
		{name: "not base64", raw: "not-a-cursor", sort: "id", reason: "garbage"},
		{name: "not json", raw: "bm90LWpzb24", sort: "id", reason: "decoded garbage"},
		{name: "wrong sort", raw: valid, sort: "-id", reason: "sort mismatch"},
		{name: "empty", raw: "", sort: "id", reason: "missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := httpx.DecodeCursor(tc.raw, tc.sort, []string{"id"})
			var problem *httpx.ProblemError
			if !errors.As(err, &problem) {
				t.Fatalf("error = %v, want ProblemError (%s)", err, tc.reason)
			}
			if problem.Status != http.StatusBadRequest || problem.Code != httpx.CodeValidationFailed {
				t.Fatalf("problem = %+v, want 400 VALIDATION_FAILED", problem)
			}
			if len(problem.Issues) != 1 || problem.Issues[0].Field != "cursor" {
				t.Errorf("issues = %+v, want the cursor issue", problem.Issues)
			}
		})
	}
}
