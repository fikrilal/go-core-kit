package httpx

import (
	"bytes"
	"context"
	"encoding/json"
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

// HandleIdempotent registers a route with Idempotency-Key caching and replay.
func (r *Router) HandleIdempotent(pattern string, h http.HandlerFunc) {
	if r.idempotency == nil {
		panic("httpx: HandleIdempotent requires WithIdempotency")
	}
	r.mux.HandleFunc(pattern, r.idempotency.wrap(pattern, h))
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

func (s *idempotencyStore) wrap(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next(w, r)
			return
		}
		s.handle(w, r, scope, key, next)
	}
}

func (s *idempotencyStore) handle(w http.ResponseWriter, r *http.Request, scope, key string, next http.HandlerFunc) {
	responseKey := redis.Key("idempotency", scope, key)
	if s.tryReplay(w, r, responseKey) {
		return
	}
	lockKey := responseKey + ":lock"
	acquired, err := s.client.SetNX(r.Context(), lockKey, RequestID(r), idempotencyLockTTL)
	if err != nil {
		WriteError(w, r, fmt.Errorf("idempotency lock: %w", err))
		return
	}
	if !acquired {
		if s.tryReplay(w, r, responseKey) {
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
	s.store(r, responseKey, recorder)
}

func (s *idempotencyStore) tryReplay(w http.ResponseWriter, r *http.Request, responseKey string) bool {
	raw, found, err := s.client.Get(r.Context(), responseKey)
	if err != nil {
		WriteError(w, r, fmt.Errorf("idempotency read: %w", err))
		return true
	}
	if !found {
		return false
	}
	var cached cachedResponse
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		WriteError(w, r, fmt.Errorf("idempotency decode: %w", err))
		return true
	}
	w.Header().Set("Idempotency-Replayed", "true")
	if cached.ContentType != "" {
		w.Header().Set("Content-Type", cached.ContentType)
	}
	if cached.Location != "" {
		w.Header().Set("Location", cached.Location)
	}
	w.WriteHeader(cached.Status)
	_, _ = w.Write(cached.Body)
	return true
}

func (s *idempotencyStore) store(r *http.Request, responseKey string, recorder *recordingWriter) {
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
	if err := s.client.Set(r.Context(), responseKey, string(payload), idempotencyTTL); err != nil {
		Logger(r).Error("store idempotent response", slog.Any("error", err))
	}
}

func (s *idempotencyStore) releaseLock(r *http.Request, lockKey string) {
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
