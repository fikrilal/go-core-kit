// Package httpx provides the platform HTTP router, middleware, and contract response helpers.
package httpx

import (
	"log/slog"
	"net/http"
	"slices"
)

// Operation identifies the contract operation a route serves.
type Operation struct {
	Method string
	Path   string
	ID     string
}

// Router serves the platform middleware chain over contract operations.
type Router struct {
	mux               *http.ServeMux
	handler           http.Handler
	operations        []Operation
	idempotency       *idempotencyStore
	idempotencyCaller func(*http.Request) (string, bool)
}

// RouterOption configures optional router behavior.
type RouterOption func(*Router)

// NewRouter builds the router with request id, access logging, panic recovery, and a body limit.
func NewRouter(logger *slog.Logger, opts ...RouterOption) *Router {
	mux := http.NewServeMux()
	mux.HandleFunc("/", notFound)
	// Auth and idempotency wrap single routes, guard outside.
	handler := bodyLimit(mux)
	handler = recoverPanics(handler)
	handler = accessLog(logger, handler)
	handler = requestID(handler)
	router := &Router{
		mux:     mux,
		handler: handler,
	}
	for _, opt := range opts {
		opt(router)
	}
	return router
}

// Handle registers a handler for a contract operation.
func (r *Router) Handle(op Operation, h http.Handler) {
	r.register(op)
	r.mux.Handle(op.pattern(), h)
}

// HandleFunc registers a handler function for a contract operation.
func (r *Router) HandleFunc(op Operation, h http.HandlerFunc) {
	r.register(op)
	r.mux.HandleFunc(op.pattern(), h)
}

// Operations returns the registered contract operations in registration order.
func (r *Router) Operations() []Operation {
	return slices.Clone(r.operations)
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.handler.ServeHTTP(w, req)
}

func (r *Router) register(op Operation) {
	r.operations = append(r.operations, op)
}

func (op Operation) pattern() string {
	return op.Method + " " + op.Path
}

func notFound(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, &ProblemError{Status: http.StatusNotFound, Code: CodeNotFound})
}
