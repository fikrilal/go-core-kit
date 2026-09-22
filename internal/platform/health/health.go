// Package health provides liveness and readiness probe handlers.
package health

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

// Check reports whether a named dependency is available.
type Check struct {
	Name   string
	Verify func(ctx context.Context) error
}

// Handler serves the health and readiness probes.
type Handler struct {
	checks []Check
}

// New builds the probe handler with the given readiness checks.
func New(checks ...Check) *Handler {
	return &Handler{checks: checks}
}

// Register mounts the probes on the router.
func (h *Handler) Register(r *httpx.Router) {
	r.HandleFunc(httpx.Operation{Method: http.MethodGet, Path: "/health", ID: "health.get"}, h.handleHealth)
	r.HandleFunc(httpx.Operation{Method: http.MethodGet, Path: "/ready", ID: "ready.get"}, h.handleReady)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.SkipAccessLog(w)
	httpx.WriteDocument(w, r, http.StatusOK, statusResponse{Status: "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.SkipAccessLog(w)
	failing := map[string]string{}
	for _, check := range h.checks {
		err := check.Verify(r.Context())
		if err == nil {
			continue
		}
		failing[check.Name] = "unavailable"
		httpx.Logger(r).Warn("readiness check failed",
			slog.String("check", check.Name),
			slog.Any("error", err),
		)
	}
	if len(failing) > 0 {
		httpx.WriteDocument(w, r, http.StatusServiceUnavailable, statusResponse{Status: "unavailable", Checks: failing})
		return
	}
	httpx.WriteDocument(w, r, http.StatusOK, statusResponse{Status: "ok"})
}

type statusResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}
