package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// featureDomainTemplate is the feature's core package.
func featureDomainTemplate(name string) string {
	return fmt.Sprintf(`// Package domain holds the %s feature's core types and rules.
package domain
`, name)
}

// featurePortsTemplate is the feature's persistence boundary.
func featurePortsTemplate(name string) string {
	return fmt.Sprintf(`// Package ports holds the %s feature's boundaries.
package ports

// Repository is the %s feature's persistence boundary.
type Repository interface{}
`, name, name)
}

// featureRepositoryTemplate is the repository over the generated queries.
func featureRepositoryTemplate(name string) string {
	return fmt.Sprintf(`//go:build !pg

// Package infra implements the %s feature's persistence.
// This build is SQLite until a pg repository exists.
package infra

import (
	"github.com/fikrilal/go-core-kit/internal/db/gen"
	"github.com/fikrilal/go-core-kit/internal/features/%s/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
)

// Repository is the %s feature's persistence boundary.
type Repository struct {
	database *db.Database
	queries  *gen.Queries
}

// NewRepository builds the repository.
func NewRepository(database *db.Database) *Repository {
	return &Repository{database: database, queries: gen.New(database.SQL())}
}

var _ ports.Repository = (*Repository)(nil)
`, name, name, name)
}

// featureServiceTemplate is the feature's use-case shell.
func featureServiceTemplate(name string) string {
	return fmt.Sprintf(`// Package app implements the %s feature's use cases.
package app

import (
	"github.com/fikrilal/go-core-kit/internal/features/%s/ports"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
)

// Deps are the service's collaborators.
type Deps struct {
	Repository ports.Repository
	Clock      clock.Clock
}

// Service implements the %s use cases.
type Service struct {
	repo  ports.Repository
	clock clock.Clock
}

// New builds the service.
func New(deps Deps) *Service {
	return &Service{repo: deps.Repository, clock: deps.Clock}
}
`, name, name, name)
}

// featureRootTemplate is the feature's HTTP surface and wiring.
func featureRootTemplate(name string) string {
	return fmt.Sprintf(`//go:build !pg

// Package %[1]s implements the %[1]s feature's HTTP surface and wiring.
// This build is SQLite until a pg repository exists.
package %[1]s

import (
	platformauth "github.com/fikrilal/go-core-kit/internal/platform/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

// Deps are the %[1]s feature's dependencies.
type Deps struct {
	Database *db.Database
	Clock    clock.Clock
	Guard    *platformauth.Guard
}

// Register mounts the %[1]s feature on the router. Routes are added by
// `+"`gokit scaffold endpoint`"+`, one route per contract operation.
func Register(router *httpx.Router, deps Deps) {
	%[2]s
	// The skeleton alone implements no operation, so nothing is mounted yet.
	_, _ = router, deps
}
`, name, routeRegistrationMarker)
}

// featureRootTestTemplate proves the skeleton mounts without routes.
func featureRootTestTemplate(name string) string {
	return fmt.Sprintf(`//go:build !pg

package %[1]s_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/fikrilal/go-core-kit/internal/features/%[1]s"
	"github.com/fikrilal/go-core-kit/internal/platform/clock"
	"github.com/fikrilal/go-core-kit/internal/platform/db"
	"github.com/fikrilal/go-core-kit/internal/platform/httpx"
)

// TestRegisterWires: Register must mount with the composition root's
// dependencies. That the feature implements no contract operation is the
// coverage test's job, not this one's.
func TestRegisterWires(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	database := db.MustOpen(filepath.Join(t.TempDir(), "feature.db"))
	t.Cleanup(func() { _ = database.Close() })
	router := httpx.NewRouter(logger)
	%[1]s.Register(router, %[1]s.Deps{Database: database, Clock: clock.System{}})
	t.Log("feature registered")
}
`, name)
}

// endpointServiceTemplate is the use case for one operation.
func endpointServiceTemplate(_ string, spec operationSpec) string {
	useCase := spec.useCase()
	return fmt.Sprintf(`package app

import (
	"context"
	"fmt"
)

// %[1]sInput carries the request the handler validated.
type %[1]sInput struct {
	UserID string
}

// %[1]sResult is the response the snapshot declares; fill in its fields.
type %[1]sResult struct {
	ID string
}

// %[1]s implements %[2]s (%[3]s %[4]s). The scaffolded body is a starting
// point: read the stored rows through the repository and return the fields the
// snapshot declares.
func (s *Service) %[1]s(ctx context.Context, in %[1]sInput) (%[1]sResult, error) {
	if err := ctx.Err(); err != nil {
		return %[1]sResult{}, fmt.Errorf("query %[2]s: %%w", err)
	}
	_ = s.clock
	_ = in
	return %[1]sResult{}, nil
}
`, useCase, spec.id, spec.method, spec.path)
}

// endpointRepositoryTemplate is the repository call the use case needs.
func endpointRepositoryTemplate(feature string, spec operationSpec) string {
	useCase := spec.useCase()
	return fmt.Sprintf(`//go:build !pg

package infra

import (
	"context"
	"fmt"
)

// %[1]sRow is the row the query returns.
type %[1]sRow struct {
	ID string
}

// %[1]s reads the row behind %[2]s (%[3]s %[4]s). The query lives in
// db/queries/common/%[5]s.sql and runs through the generated queries.
func (r *Repository) %[1]s(ctx context.Context) (%[1]sRow, bool, error) {
	if err := ctx.Err(); err != nil {
		return %[1]sRow{}, false, fmt.Errorf("read %[2]s: %%w", err)
	}
	_ = r.queries
	return %[1]sRow{}, false, nil
}
`, useCase, spec.id, spec.method, spec.path, feature)
}

// endpointServiceTestTemplate covers the scaffolded use case.
func endpointServiceTestTemplate(feature string, spec operationSpec) string {
	useCase := spec.useCase()
	return fmt.Sprintf(`package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/fikrilal/go-core-kit/internal/features/%[1]s/app"
)

// stubClock is a fixed clock for the scaffolded use case.
type stubClock struct{ now time.Time }

func (c stubClock) Now() time.Time { return c.now }

func Test%[2]sRuns(t *testing.T) {
	service := app.New(app.Deps{Clock: stubClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}})
	if _, err := service.%[2]s(context.Background(), app.%[2]sInput{}); err != nil {
		t.Fatalf("%%s() error = %%v", "%[2]s", err)
	}
}
`, feature, useCase)
}

// endpointQueryTemplate is the SQL the repository runs.
func endpointQueryTemplate(spec operationSpec) string {
	return fmt.Sprintf(`-- name: %s :one
-- TODO(%s): select the columns %s (%s %s) returns.
SELECT id FROM users WHERE id = sqlc.arg(id);
`, spec.function, spec.id, spec.id, spec.method, spec.path)
}

// endpointContractTestTemplate is the contract-test skeleton for the new route.
// It names the operation and the fixture work the caller must finish, and skips
// until then so the suite stays honest.
func endpointContractTestTemplate(_ string, spec operationSpec) string {
	return fmt.Sprintf(`//go:build !pg

package main

import "testing"

// TestContract%s exercises %s (%s %s) through the real server: build the
// contract fixture, call the route with and without a session, and assert the
// problem codes the snapshot declares.
func TestContract%s(t *testing.T) {
	t.Skip("TODO(%s): drive %s through newServer with the contract fixture")
}
`, spec.function, spec.id, spec.method, spec.path, spec.function, spec.id, spec.id)
}

// registerRoute adds the handler and route for one operation to the feature
// root, keeping the wiring inside the route markers.
func registerRoute(dir, feature string, spec operationSpec) error {
	rootPath := filepath.Join(dir, feature+".go")
	// #nosec G304 -- the path derives from the validated feature name.
	data, err := os.ReadFile(rootPath)
	if err != nil {
		return fmt.Errorf("scaffold: read %s: %w", rootPath, err)
	}
	content := string(data)
	if !strings.Contains(content, routeRegistrationMarker) {
		return usagef("scaffold: %s has no %q marker", rootPath, routeRegistrationMarker)
	}
	if strings.Contains(content, `ID: "`+spec.id+`"`) {
		return usagef("scaffold: %s already routes %s", rootPath, spec.id)
	}

	wiring := "\tservice := app.New(app.Deps{Repository: infra.NewRepository(deps.Database), Clock: deps.Clock})\n" +
		"\thandlers := &handlers{service: service}\n"
	placeholder := "\t// The skeleton alone implements no operation, so nothing is mounted yet.\n\t_, _ = router, deps\n"
	operation := fmt.Sprintf("{Method: http.Method%s, Path: %q, ID: %q}", titleFirst(strings.ToLower(spec.method)), spec.path, spec.id)
	handlerRoute := fmt.Sprintf("\t%sOperation := httpx.Operation%s\n\trouter.Handle(%sOperation, deps.Guard.Require(http.HandlerFunc(handlers.%s)))\n",
		spec.function, operation, spec.function, spec.function)
	// The route must follow the wiring it uses, and the wiring follows the
	// marker, so insert the block in one place.
	content = strings.Replace(content, placeholder, wiring+handlerRoute, 1)
	if !strings.Contains(content, handlerRoute) {
		anchor := "\thandlers := &handlers{service: service}\n"
		content = strings.Replace(content, anchor, anchor+handlerRoute, 1)
	}
	// A second endpoint on the same feature must not re-declare the imports or
	// the handler set, and only the first one brings net/http in.
	if !strings.Contains(content, "internal/features/"+feature+"/app\"") {
		content = strings.Replace(content, "\t\"github.com/fikrilal/go-core-kit/internal/platform/httpx\"",
			"\t\"github.com/fikrilal/go-core-kit/internal/features/"+feature+"/app\"\n"+
				"\t\"github.com/fikrilal/go-core-kit/internal/features/"+feature+"/infra\"\n"+
				"\t\"github.com/fikrilal/go-core-kit/internal/platform/httpx\"", 1)
	}
	if !strings.Contains(content, "\t\"net/http\"\n") {
		content = strings.Replace(content, "import (\n", "import (\n\t\"net/http\"\n\n", 1)
	}
	content = strings.TrimRight(content, "\n") + "\n" + handlerTemplate(spec)
	if !strings.Contains(content, "type handlers struct") {
		content += handlersStructTemplate()
	}
	formatted, err := formatGo(rootPath, content)
	if err != nil {
		return err
	}
	// #nosec G703 -- the path derives from the validated feature name.
	if err := os.WriteFile(rootPath, []byte(formatted), 0o600); err != nil {
		return fmt.Errorf("scaffold: write %s: %w", rootPath, err)
	}
	return nil
}

// handlerTemplate is the HTTP handler for one operation.
func handlerTemplate(spec operationSpec) string {
	return fmt.Sprintf(`
// %s answers %s (%s %s) with the kit's response envelope.
func (h *handlers) %s(w http.ResponseWriter, r *http.Request) {
	principal, ok := platformauth.PrincipalFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, &httpx.ProblemError{Status: http.StatusUnauthorized, Code: httpx.CodeUnauthorized})
		return
	}
	result, err := h.service.%s(r.Context(), app.%sInput{UserID: principal.UserID})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Data(w, r, result)
}
`, spec.function, spec.id, spec.method, spec.path, spec.function, spec.useCase(), spec.useCase())
}

// handlersStructTemplate is the handler set the feature root needs.
func handlersStructTemplate() string {
	return `
type handlers struct {
	service *app.Service
}
`
}
