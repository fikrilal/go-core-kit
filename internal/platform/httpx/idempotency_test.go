package httpx_test

import (
	"context"
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
	router := httpx.NewRouter(logger, httpx.WithIdempotency(client))
	router.SetIdempotencyCaller(func(*http.Request) (string, bool) {
		return testCallerID, true
	})
	return router, server
}

const testCallerID = "user-1"

type testCallerKey struct{}

func requestAs(t *testing.T, key, userID string) *http.Request {
	t.Helper()
	req := idempotentRequest(t, key)
	if userID == "" {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), testCallerKey{}, userID))
}

func callerFromContext(r *http.Request) (string, bool) {
	id, _ := r.Context().Value(testCallerKey{}).(string)
	if id == "" {
		return "", false
	}
	return id, true
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
	router.HandleIdempotent(testOp("POST", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
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
	router.HandleIdempotent(testOp("POST", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
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
	router.HandleIdempotent(testOp("POST", "/v1/things"), func(w http.ResponseWriter, _ *http.Request) {
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
	router.HandleIdempotent(testOp("POST", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
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
	router.HandleIdempotent(testOp("POST", "/v1/things"), func(w http.ResponseWriter, r *http.Request) {
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
	op := testOp("POST", "/v1/things")
	router.HandleIdempotent(op, func(w http.ResponseWriter, _ *http.Request) {
		httpx.NoContent(w)
	})

	router.ServeHTTP(httptest.NewRecorder(), idempotentRequest(t, "key-1"))

	lockKey := redis.Key("idempotency", op.ID, testCallerID, "key-1") + ":lock"
	if server.Exists(lockKey) {
		t.Errorf("lock key %q still present after completion", lockKey)
	}
}

func TestIdempotencyScopesByCaller(t *testing.T) {
	router, server := testIdempotentRouter(t)
	router.SetIdempotencyCaller(callerFromContext)
	calls := 0
	op := testOp("POST", "/v1/things")
	router.HandleIdempotent(op, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		httpx.NoContent(w)
	})

	router.ServeHTTP(httptest.NewRecorder(), requestAs(t, "key-1", "user-a"))
	router.ServeHTTP(httptest.NewRecorder(), requestAs(t, "key-1", "user-b"))
	replay := httptest.NewRecorder()
	router.ServeHTTP(replay, requestAs(t, "key-1", "user-a"))

	if calls != 2 {
		t.Errorf("handler calls = %d, want 2", calls)
	}
	if replay.Header().Get("Idempotency-Replayed") != "true" {
		t.Errorf("Idempotency-Replayed = %q, want true", replay.Header().Get("Idempotency-Replayed"))
	}
	keyA := redis.Key("idempotency", op.ID, "user-a", "key-1")
	keyB := redis.Key("idempotency", op.ID, "user-b", "key-1")
	if !server.Exists(keyA) || !server.Exists(keyB) {
		t.Errorf("keys = %v, want %s and %s", server.Keys(), keyA, keyB)
	}
}

func TestIdempotencyMissingCallerDoesNotRun(t *testing.T) {
	router, server := testIdempotentRouter(t)
	router.SetIdempotencyCaller(func(*http.Request) (string, bool) {
		return "", false
	})
	calls := 0
	router.HandleIdempotent(testOp("POST", "/v1/things"), func(w http.ResponseWriter, _ *http.Request) {
		calls++
		httpx.NoContent(w)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, idempotentRequest(t, "key-1"))
	router.ServeHTTP(httptest.NewRecorder(), idempotentRequest(t, ""))

	if calls != 0 {
		t.Errorf("handler calls = %d, want 0", calls)
	}
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
	if keys := server.Keys(); len(keys) != 0 {
		t.Errorf("keys = %v, want none", keys)
	}
}

func TestHandleIdempotentRequiresConfiguration(t *testing.T) {
	router, _ := testRouter()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic without WithIdempotency")
		}
	}()
	router.HandleIdempotent(testOp("POST", "/v1/x"), func(http.ResponseWriter, *http.Request) {})
}

func TestHandleIdempotentRequiresCaller(t *testing.T) {
	logger := logging.New(config.LogLevelError, config.LogFormatJSON, io.Discard)
	server := miniredis.RunT(t)
	client := redis.MustOpen("redis://" + server.Addr() + "/0")
	router := httpx.NewRouter(logger, httpx.WithIdempotency(client))
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic without SetIdempotencyCaller")
		}
	}()
	router.HandleIdempotent(testOp("POST", "/v1/x"), func(http.ResponseWriter, *http.Request) {})
}
