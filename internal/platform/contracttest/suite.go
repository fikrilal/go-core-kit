// Package contracttest validates live HTTP responses against the pinned OpenAPI snapshot.
package contracttest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"
)

// SnapshotEnv overrides the pinned snapshot path (gate-honesty injections use it).
const SnapshotEnv = "CONTRACT_SNAPSHOT_PATH"

const (
	snapshotDefault  = "contracts/backend.openapi.yaml"
	problemMediaType = "application/problem+json"
	requestIDHeader  = "X-Request-Id"
)

// Response is a fully read live response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Suite validates live responses against the pinned snapshot.
type Suite struct {
	t      *testing.T
	spec   *openapi3.T
	router routers.Router
}

// New loads and validates the pinned snapshot and builds its request router.
func New(t *testing.T) *Suite {
	t.Helper()
	path := os.Getenv(SnapshotEnv)
	if path == "" {
		path = filepath.Join(repoRoot(t), snapshotDefault)
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	spec, err := loader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("contracttest: load snapshot: %v", err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatalf("contracttest: snapshot does not validate: %v", err)
	}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatalf("contracttest: build snapshot router: %v", err)
	}
	return &Suite{t: t, spec: spec, router: router}
}

// SnapshotOperation is an operation declared by the pinned snapshot.
type SnapshotOperation struct {
	ID     string
	Method string
	Path   string
}

// SnapshotOperations lists every operation the snapshot declares.
func (s *Suite) SnapshotOperations() []SnapshotOperation {
	var operations []SnapshotOperation
	for path, item := range s.spec.Paths.Map() {
		for method, operation := range item.Operations() {
			operations = append(operations, SnapshotOperation{
				ID:     operation.OperationID,
				Method: strings.ToUpper(method),
				Path:   path,
			})
		}
	}
	return operations
}

// RepositoryPath resolves a repository-relative path from any package directory.
func RepositoryPath(t *testing.T, relative string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), relative)
}

// ValidateResponse checks a live response against the snapshot operation schema.
func (s *Suite) ValidateResponse(req *http.Request, resp Response) {
	s.t.Helper()
	route, pathParams, err := s.router.FindRoute(req)
	if err != nil {
		s.t.Fatalf("contracttest: %s %s: no matching operation in the snapshot: %v", req.Method, req.URL.Path, err)
	}
	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: req, Route: route, PathParams: pathParams},
		Status:                 resp.Status,
		Header:                 resp.Header,
		Body:                   io.NopCloser(bytes.NewReader(resp.Body)),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		s.t.Fatalf("contracttest: %s %s %d: %v\nbody: %s", req.Method, req.URL.Path, resp.Status, err, resp.Body)
	}
}

// AssertErrorCodeDeclared checks membership in the operation's x-error-codes.
func (s *Suite) AssertErrorCodeDeclared(operationID, code string) {
	s.t.Helper()
	operation := s.operation(operationID)
	raw, ok := operation.Extensions["x-error-codes"]
	if !ok {
		s.t.Fatalf("contracttest: operation %q declares no x-error-codes", operationID)
	}
	declared := stringList(raw)
	if !slices.Contains(declared, code) {
		s.t.Fatalf("contracttest: operation %q does not declare error code %q (declares %v)", operationID, code, declared)
	}
}

func (s *Suite) operation(operationID string) *openapi3.Operation {
	for _, item := range s.spec.Paths.Map() {
		for _, operation := range item.Operations() {
			if operation.OperationID == operationID {
				return operation
			}
		}
	}
	s.t.Fatalf("contracttest: operation %q not found in the snapshot", operationID)
	return nil
}

// Do performs req and returns the response with its body fully read and closed.
func Do(t *testing.T, client *http.Client, req *http.Request) Response {
	t.Helper()
	resp, err := client.Do(req) //nolint:gosec // tests target the local httptest server
	if err != nil {
		t.Fatalf("contracttest: %s %s: %v", req.Method, req.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("contracttest: read %s %s body: %v", req.Method, req.URL, err)
	}
	return Response{Status: resp.StatusCode, Header: resp.Header, Body: body}
}

// AssertRequestID requires a well-formed X-Request-Id echo.
func AssertRequestID(t *testing.T, resp Response) {
	t.Helper()
	id := resp.Header.Get(requestIDHeader)
	if id == "" {
		t.Fatalf("contracttest: %s header missing", requestIDHeader)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("contracttest: %s = %q, not a UUID: %v", requestIDHeader, id, err)
	}
}

// AssertProblem requires an RFC7807 response carrying wantCode and the request id.
func AssertProblem(t *testing.T, resp Response, wantCode string) {
	t.Helper()
	if resp.Status < 400 {
		t.Fatalf("contracttest: expected an error status, got %d", resp.Status)
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, problemMediaType) {
		t.Fatalf("contracttest: content type = %q, want %s", contentType, problemMediaType)
	}
	var problem struct {
		Type    string `json:"type"`
		Status  int    `json:"status"`
		Code    string `json:"code"`
		TraceID string `json:"traceId"`
	}
	if err := json.Unmarshal(resp.Body, &problem); err != nil {
		t.Fatalf("contracttest: decode problem: %v\nbody: %s", err, resp.Body)
	}
	if problem.Code != wantCode {
		t.Fatalf("contracttest: problem code = %q, want %q\nbody: %s", problem.Code, wantCode, resp.Body)
	}
	if problem.Status != resp.Status {
		t.Fatalf("contracttest: problem status = %d, want %d", problem.Status, resp.Status)
	}
	if problem.Type == "" || problem.TraceID == "" {
		t.Fatalf("contracttest: problem missing type or traceId: %s", resp.Body)
	}
	if want := resp.Header.Get(requestIDHeader); problem.TraceID != want {
		t.Fatalf("contracttest: problem traceId = %q, want %q", problem.TraceID, want)
	}
}

func stringList(raw any) []string {
	switch value := raw.(type) {
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case []string:
		return value
	}
	return nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("contracttest: working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("contracttest: repository root not found from %s", dir)
		}
		dir = parent
	}
}
