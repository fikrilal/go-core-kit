package httpx_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
	"github.com/fikrilal/go-core-kit/internal/platform/logging"
	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

func testIdempotentRouter(t *testing.T) (*httpx.Router, *miniredis.Miniredis) {
	t.Helper()
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	server := miniredis.RunT(t)
	client := redis.MustOpen("redis://" + server.Addr() + "/0")
	return httpx.NewRouter(logger, httpx.WithIdempotency(client)), server
}

func idempotentRequest(t *testing.T, key string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/things", nil)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req
}

func TestIdempotencyReplaysCachedResponse(t *testing.T) {
	router, _ := testIdempotentRouter(t)
	calls := 0
	router.HandleIdempotent("POST /v1/things", func(w http.ResponseWriter, r *http.Request) {
		calls++
		httpx.Created(w, r, "/v1/things/1", "created")
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, idempotentRequest(t, "key-1"))
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d, want 201", first.Code)
	}
	if first.Header().Get("Idempotency-Replayed") != "" {
		t.Error("first response must not be marked replayed")
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, idempotentRequest(t, "key-1"))

	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
	if second.Code != http.StatusCreated {
		t.Errorf("replay status = %d, want 201", second.Code)
	}
	if got := second.Header().Get("Idempotency-Replayed"); got != "true" {
		t.Errorf("Idempotency-Replayed = %q, want true", got)
	}
	if got := second.Header().Get("Location"); got != "/v1/things/1" {
		t.Errorf("replay Location = %q, want /v1/things/1", got)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("replay body = %q, want %q", second.Body.String(), first.Body.String())
	}
}

func TestIdempotencyDifferentKeysRunSeparately(t *testing.T) {
	router, _ := testIdempotentRouter(t)
	calls := 0
	router.HandleIdempotent("POST /v1/things", func(w http.ResponseWriter, r *http.Request) {
		calls++
		httpx.Data(w, r, calls)
	})

	router.ServeHTTP(httptest.NewRecorder(), idempotentRequest(t, "key-a"))
	router.ServeHTTP(httptest.NewRecorder(), idempotentRequest(t, "key-b"))

	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
}

func TestIdempotencyWithoutKeyPassesThrough(t *testing.T) {
	router, _ := testIdempotentRouter(t)
	calls := 0
	router.HandleIdempotent("POST /v1/things", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		httpx.NoContent(w)
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, idempotentRequest(t, ""))
	second := httptest.NewRecorder()
	router.ServeHTTP(second, idempotentRequest(t, ""))

	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
	if second.Header().Get("Idempotency-Replayed") != "" {
		t.Error("unkeyed request must not be marked replayed")
	}
}

func TestIdempotencyInProgressReturns409(t *testing.T) {
	router, _ := testIdempotentRouter(t)
	started := make(chan struct{})
	release := make(chan struct{})
	router.HandleIdempotent("POST /v1/things", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		httpx.Data(w, r, "done")
	})

	firstReq := idempotentRequest(t, "key-1")
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		router.ServeHTTP(httptest.NewRecorder(), firstReq)
	}()

	<-started
	second := httptest.NewRecorder()
	router.ServeHTTP(second, idempotentRequest(t, "key-1"))

	if second.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", second.Code)
	}
	var body problemResponse
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != "IDEMPOTENCY_IN_PROGRESS" {
		t.Errorf("code = %q, want IDEMPOTENCY_IN_PROGRESS", body.Code)
	}

	close(release)
	<-firstDone
}

func TestIdempotencyDoesNotCacheErrors(t *testing.T) {
	router, _ := testIdempotentRouter(t)
	calls := 0
	router.HandleIdempotent("POST /v1/things", func(w http.ResponseWriter, r *http.Request) {
		calls++
		httpx.WriteError(w, r, &httpx.ProblemError{
			Status: http.StatusBadRequest,
			Code:   httpx.CodeValidationFailed,
			Detail: "nope",
		})
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, idempotentRequest(t, "key-1"))
	second := httptest.NewRecorder()
	router.ServeHTTP(second, idempotentRequest(t, "key-1"))

	if calls != 2 {
		t.Errorf("handler calls = %d, want 2 (errors are not cached)", calls)
	}
	if second.Header().Get("Idempotency-Replayed") != "" {
		t.Error("error response must not be replayed")
	}
}

func TestIdempotencyReleasesLock(t *testing.T) {
	router, server := testIdempotentRouter(t)
	router.HandleIdempotent("POST /v1/things", func(w http.ResponseWriter, _ *http.Request) {
		httpx.NoContent(w)
	})

	router.ServeHTTP(httptest.NewRecorder(), idempotentRequest(t, "key-1"))

	lockKey := redis.Key("idempotency", "POST /v1/things", "key-1") + ":lock"
	if server.Exists(lockKey) {
		t.Errorf("lock key %q still present after completion", lockKey)
	}
}

func TestHandleIdempotentRequiresConfiguration(t *testing.T) {
	router, _ := testRouter()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic without WithIdempotency")
		}
	}()
	router.HandleIdempotent("POST /v1/x", func(http.ResponseWriter, *http.Request) {})
}
