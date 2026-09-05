// Package httpx provides the platform HTTP router, middleware, and contract response helpers.
package httpx

import (
	"log/slog"
	"net/http"
)

// Router serves the platform middleware chain over pattern routes.
type Router struct {
	mux     *http.ServeMux
	handler http.Handler
}

// NewRouter builds the router with request id, access logging, and panic recovery.
func NewRouter(logger *slog.Logger) *Router {
	mux := http.NewServeMux()
	mux.HandleFunc("/", notFound)
	return &Router{
		mux:     mux,
		handler: requestID(accessLog(logger, recoverPanics(mux))),
	}
}

// Handle registers a handler for a method and pattern route.
func (r *Router) Handle(pattern string, h http.Handler) {
	r.mux.Handle(pattern, h)
}

// HandleFunc registers a handler function for a method and pattern route.
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.mux.HandleFunc(pattern, h)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, &ProblemError{Status: http.StatusNotFound, Code: CodeNotFound})
}
