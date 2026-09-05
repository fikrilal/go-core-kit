package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/fikrilal/go-core-kit/internal/platform/logging"
)

type contextKey int

const (
	ctxKeyRequestID contextKey = iota
	ctxKeyLogger
)

// RequestID returns the correlation id for the request.
func RequestID(r *http.Request) string {
	id, _ := r.Context().Value(ctxKeyRequestID).(string)
	return id
}

// Logger returns the request-scoped logger with correlation fields bound.
func Logger(r *http.Request) *slog.Logger {
	if logger, ok := r.Context().Value(ctxKeyLogger).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

func accessLog(base *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		logger := base.With(slog.String(logging.FieldRequestID, RequestID(r)))
		r = r.WithContext(context.WithValue(r.Context(), ctxKeyLogger, logger))

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		level := slog.LevelInfo
		if sw.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				Logger(r).Error("panic recovered",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				WriteError(w, r, &ProblemError{Status: http.StatusInternalServerError, Code: CodeInternal})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b) //nolint:wrapcheck // io.Writer pairs must pass the underlying error through
}
