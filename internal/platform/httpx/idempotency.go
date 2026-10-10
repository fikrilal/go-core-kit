package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/fikrilal/go-core-kit/internal/platform/redis"
)

const (
	idempotencyTTL     = 24 * time.Hour
	idempotencyLockTTL = 30 * time.Second
)

// WithIdempotency enables Idempotency-Key handling for routes registered with HandleIdempotent.
func WithIdempotency(client *redis.Client) RouterOption {
	return func(r *Router) {
		r.idempotency = &idempotencyStore{client: client}
	}
}

// SetIdempotencyCaller registers the caller id that scopes idempotency keys.
func (r *Router) SetIdempotencyCaller(caller func(*http.Request) (string, bool)) {
	r.idempotencyCaller = caller
}

// Idempotent wraps h with caller-scoped Idempotency-Key caching and replay.
// Compose it inside a guard. A request with no caller fails closed.
func (r *Router) Idempotent(op Operation, h http.HandlerFunc) http.Handler {
	return r.IdempotentFor(op, idempotencyTTL, h)
}

// IdempotentFor is Idempotent with a custom replay TTL: a cached response must
// not outlive what it handed out (for example a presigned upload URL).
func (r *Router) IdempotentFor(op Operation, ttl time.Duration, h http.HandlerFunc) http.Handler {
	if r.idempotency == nil {
		panic("httpx: Idempotent requires WithIdempotency")
	}
	if r.idempotencyCaller == nil {
		panic("httpx: Idempotent requires SetIdempotencyCaller")
	}
	return r.idempotency.wrap(op.ID, r.idempotencyCaller, ttl, h)
}

// HandleIdempotent registers a contract operation with Idempotency-Key caching and replay.
func (r *Router) HandleIdempotent(op Operation, h http.HandlerFunc) {
	r.Handle(op, r.Idempotent(op, h))
}

type idempotencyStore struct {
	client *redis.Client
}

type cachedResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Location    string `json:"location,omitempty"`
	Body        []byte `json:"body"`
}

func (s *idempotencyStore) wrap(scope string, caller func(*http.Request) (string, bool), ttl time.Duration, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := caller(r)
		if !ok || userID == "" {
			WriteError(w, r, errors.New("idempotency caller missing"))
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next(w, r)
			return
		}
		s.handle(w, r, scope, userID, key, ttl, next)
	}
}

func (s *idempotencyStore) handle(w http.ResponseWriter, r *http.Request, scope, userID, key string, ttl time.Duration, next http.HandlerFunc) {
	responseKey := idempotencyKey(scope, userID, key)
	cached, found, err := s.loadCached(r.Context(), responseKey)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if found {
		writeCached(w, cached)
		return
	}
	lockKey := responseKey + ":lock"
	acquired, err := s.client.SetNX(r.Context(), lockKey, RequestID(r), idempotencyLockTTL)
	if err != nil {
		WriteError(w, r, fmt.Errorf("idempotency lock: %w", err))
		return
	}
	if !acquired {
		cached, found, err = s.loadCached(r.Context(), responseKey)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		if found {
			writeCached(w, cached)
			return
		}
		WriteError(w, r, &ProblemError{
			Status: http.StatusConflict,
			Code:   CodeIdempotencyInProgress,
			Detail: "a request with this Idempotency-Key is already in progress",
		})
		return
	}
	defer s.releaseLock(r, lockKey)

	recorder := &recordingWriter{ResponseWriter: w, status: http.StatusOK}
	next(recorder, r)
	s.store(r, responseKey, recorder, ttl)
}

// idempotencyKey is idempotency:<operation id>:<user id>:<header>.
// A body fingerprint is deferred (plan 1.8). The same key with a different body still replays.
func idempotencyKey(operationID, userID, header string) string {
	return redis.Key("idempotency", operationID, userID, header)
}

func (s *idempotencyStore) loadCached(ctx context.Context, responseKey string) (cachedResponse, bool, error) {
	raw, found, err := s.client.Get(ctx, responseKey)
	if err != nil {
		return cachedResponse{}, false, fmt.Errorf("idempotency read: %w", err)
	}
	if !found {
		return cachedResponse{}, false, nil
	}
	var cached cachedResponse
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		return cachedResponse{}, false, fmt.Errorf("idempotency decode: %w", err)
	}
	return cached, true, nil
}

func writeCached(w http.ResponseWriter, cached cachedResponse) {
	w.Header().Set("Idempotency-Replayed", "true")
	if cached.ContentType != "" {
		w.Header().Set("Content-Type", cached.ContentType)
	}
	if cached.Location != "" {
		w.Header().Set("Location", cached.Location)
	}
	w.WriteHeader(cached.Status)
	_, _ = w.Write(cached.Body)
}

func (s *idempotencyStore) store(r *http.Request, responseKey string, recorder *recordingWriter, ttl time.Duration) {
	if recorder.status < http.StatusOK || recorder.status >= http.StatusMultipleChoices {
		return
	}
	cached := cachedResponse{
		Status:      recorder.status,
		ContentType: recorder.Header().Get("Content-Type"),
		Location:    recorder.Header().Get("Location"),
		Body:        recorder.body.Bytes(),
	}
	payload, _ := json.Marshal(cached)
	if err := s.client.Set(r.Context(), responseKey, string(payload), ttl); err != nil {
		Logger(r).Error("store idempotent response", slog.Any("error", err))
	}
}

func (s *idempotencyStore) releaseLock(r *http.Request, lockKey string) {
	// WithoutCancel so a disconnected client still deletes the lock.
	if err := s.client.Del(context.WithoutCancel(r.Context()), lockKey); err != nil {
		Logger(r).Error("release idempotency lock", slog.Any("error", err))
	}
}

type recordingWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
	body   bytes.Buffer
}

func (w *recordingWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	w.body.Write(b)
	return w.ResponseWriter.Write(b) //nolint:wrapcheck // io.Writer pairs must pass the underlying error through
}
