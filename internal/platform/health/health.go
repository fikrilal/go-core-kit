// Package health provides liveness and readiness probe handlers.
package health

import (
	"context"
	"encoding/json"
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
	logger *slog.Logger
	checks []Check
}

// New builds the probe handler with the given readiness checks.
func New(logger *slog.Logger, checks ...Check) *Handler {
	return &Handler{logger: logger, checks: checks}
}

// Register mounts the probes on the router.
func (h *Handler) Register(r *httpx.Router) {
	r.HandleFunc("GET /health", h.handleHealth)
	r.HandleFunc("GET /ready", h.handleReady)
}

func (h *Handler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.SkipAccessLog(w)
	writeStatus(w, http.StatusOK, statusResponse{Status: "ok"})
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
		h.logger.Warn("readiness check failed",
			slog.String("check", check.Name),
			slog.Any("error", err),
		)
	}
	if len(failing) > 0 {
		writeStatus(w, http.StatusServiceUnavailable, statusResponse{Status: "unavailable", Checks: failing})
		return
	}
	writeStatus(w, http.StatusOK, statusResponse{Status: "ok"})
}

type statusResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func writeStatus(w http.ResponseWriter, status int, body statusResponse) {
	encoded, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}
