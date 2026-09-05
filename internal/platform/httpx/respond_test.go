package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

type item struct {
	Name string `json:"name"`
}

func TestDataWritesEnvelope(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("GET /v1/thing", func(w http.ResponseWriter, r *http.Request) {
		httpx.Data(w, r, item{Name: "n1"})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/thing", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Data item `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Data.Name != "n1" {
		t.Errorf("data.name = %q, want n1", body.Data.Name)
	}
}

func TestListWritesEnvelopeWithMeta(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("GET /v1/things", func(w http.ResponseWriter, r *http.Request) {
		httpx.List(w, r, []item{{Name: "a"}}, httpx.ListMeta{NextCursor: "c1", Limit: 25, HasMore: true})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/things", nil))

	var body struct {
		Data []item         `json:"data"`
		Meta httpx.ListMeta `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Data) != 1 || body.Meta.NextCursor != "c1" || body.Meta.Limit != 25 || !body.Meta.HasMore {
		t.Errorf("unexpected list envelope: %+v", body)
	}
}

func TestListOmitsEmptyCursor(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("GET /v1/things", func(w http.ResponseWriter, r *http.Request) {
		httpx.List(w, r, []item{}, httpx.ListMeta{Limit: 25})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/things", nil))

	if strings.Contains(rec.Body.String(), "nextCursor") {
		t.Errorf("nextCursor should be omitted when empty: %s", rec.Body.String())
	}
}

func TestCreatedSetsLocation(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("POST /v1/thing", func(w http.ResponseWriter, r *http.Request) {
		httpx.Created(w, r, "/v1/thing/1", item{Name: "n1"})
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/thing", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/v1/thing/1" {
		t.Errorf("Location = %q, want /v1/thing/1", got)
	}
}

func TestNoContent(t *testing.T) {
	router, _ := testRouter()
	router.HandleFunc("DELETE /v1/thing", func(w http.ResponseWriter, _ *http.Request) {
		httpx.NoContent(w)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/thing", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
}
