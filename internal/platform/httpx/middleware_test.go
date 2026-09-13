package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

func TestRequestIDEchoesProvided(t *testing.T) {
	router, _ := testRouter()
	var seen string
	router.HandleFunc(testOp("GET", "/id"), func(w http.ResponseWriter, r *http.Request) {
		seen = httpx.RequestID(r)
		httpx.NoContent(w)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/id", nil)
	req.Header.Set("X-Request-Id", "provided-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if seen != "provided-1" {
		t.Errorf("handler saw id %q, want provided-1", seen)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "provided-1" {
		t.Errorf("response id = %q, want provided-1", got)
	}
}

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc(testOp("GET", "/id"), func(_ http.ResponseWriter, _ *http.Request) {})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/id", nil))
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/id", nil))

	idOne := first.Header().Get("X-Request-Id")
	idTwo := second.Header().Get("X-Request-Id")
	if idOne == "" || idTwo == "" {
		t.Fatalf("generated ids missing: %q, %q", idOne, idTwo)
	}
	if idOne == idTwo {
		t.Errorf("generated ids should differ: %q", idOne)
	}
}

func TestAccessLogEntry(t *testing.T) {
	router, logs := testRouter()
	router.HandleFunc(testOp("GET", "/log"), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/log", nil)
	req.Header.Set("X-Request-Id", "req-9")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v\n%s", err, logs.String())
	}
	if entry["msg"] != "http request" || entry["method"] != "GET" || entry["path"] != "/log" {
		t.Errorf("unexpected access log: %+v", entry)
	}
	if entry["status"] != float64(http.StatusTeapot) || entry["request_id"] != "req-9" {
		t.Errorf("unexpected access log correlation: %+v", entry)
	}
	if _, ok := entry["duration"]; !ok {
		t.Errorf("duration missing from access log: %+v", entry)
	}
}

func TestPanicRecovered(t *testing.T) {
	router, logs := testRouter()
	router.HandleFunc(testOp("GET", "/panic"), func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "INTERNAL" {
		t.Errorf("code = %q, want INTERNAL", body.Code)
	}
	if !strings.Contains(logs.String(), "panic recovered") || !strings.Contains(logs.String(), "kaboom") {
		t.Errorf("panic not logged: %s", logs.String())
	}
}

func TestNotFoundUsesProblemDetails(t *testing.T) {
	router, _ := testRouter()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body problemResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "NOT_FOUND" {
		t.Errorf("code = %q, want NOT_FOUND", body.Code)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id missing on 404")
	}
}

func TestSkipAccessLogMarksRequest(t *testing.T) {
	router, logs := testRouter()
	router.HandleFunc(testOp("GET", "/quiet"), func(w http.ResponseWriter, _ *http.Request) {
		httpx.SkipAccessLog(w)
		httpx.NoContent(w)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/quiet", nil))

	if logs.Len() != 0 {
		t.Errorf("logs = %q, want empty", logs.String())
	}
}

func TestRequestBodyLimit(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc(testOp("POST", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := httpx.Decode(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.Data(w, r, in.Name)
	})

	body := `{"name":"` + strings.Repeat("a", 1<<20) + `"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/things", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an oversized body", rec.Code)
	}
}
