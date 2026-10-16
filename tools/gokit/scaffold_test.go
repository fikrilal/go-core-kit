package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// scaffoldFixture is a miniature repository with just the files the scaffold
// edits: the composition root marker, the allowlist, and a pinned snapshot.
func scaffoldFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(serverPath, `package main

import (
	"net/http"

	"github.com/fikrilal/go-core-kit/internal/features/auth"
	"github.com/fikrilal/go-core-kit/internal/platform/database"
)

func newServer(router *http.ServeMux, database *database.Database) {
	guard, err := auth.Register(router, auth.Deps{})
	_ = guard
	_ = err
	_ = database
	// gokit:feature-registrations — `+"`gokit scaffold feature`"+` inserts here.
}
`)
	write(allowlistDefaultPath, testAllowlist)
	write(snapshotDefaultPath, testSnapshot)
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDir(t)) })
	return root
}

func oldWorkingDir(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return dir
}

const testSnapshot = `openapi: 3.1.0
paths:
  /v1/reports:
    get:
      operationId: reports.list
      responses:
        '200':
          description: ok
  /v1/reports/{reportId}:
    get:
      operationId: reports.get
      responses:
        '200':
          description: ok
`

const testAllowlist = `# Operations declared by the pinned snapshot that this kit does not implement yet.
operations:
  - reports.list
  - reports.get
`

func TestScaffoldFeatureCreatesSkeletonAndWiresIt(t *testing.T) {
	root := scaffoldFixture(t)

	if err := scaffoldFeature("reports"); err != nil {
		t.Fatalf("scaffoldFeature() error = %v", err)
	}
	for _, path := range []string{
		"internal/features/reports/reports.go",
		"internal/features/reports/reports_test.go",
		"internal/features/reports/domain/domain.go",
		"internal/features/reports/ports/ports.go",
		"internal/features/reports/infra/repository.go",
		"internal/features/reports/app/service.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("%s missing: %v", path, err)
		}
	}
	server := readFile(t, serverPath)
	if !strings.Contains(server, `"github.com/fikrilal/go-core-kit/internal/features/reports"`) {
		t.Error("composition root does not import the new feature")
	}
	if !strings.Contains(server, "reports.Register(router, reports.Deps{") {
		t.Error("composition root does not register the new feature")
	}
	if strings.Count(server, featureRegistrationMarker) != 1 {
		t.Error("the marker must survive the insertion")
	}
}

func TestScaffoldRefusesBadInput(t *testing.T) {
	scaffoldFixture(t)

	if err := scaffoldFeature("Reports-2"); err == nil {
		t.Error("scaffoldFeature(non-identifier) error = nil, want a refusal")
	}
	if err := scaffoldFeature("reports"); err != nil {
		t.Fatalf("scaffoldFeature() error = %v", err)
	}
	if err := scaffoldFeature("reports"); err == nil {
		t.Error("scaffoldFeature(existing) error = nil, want a refusal")
	}
	if err := scaffoldEndpoint("missing", "reports.list"); err == nil {
		t.Error("scaffoldEndpoint(missing feature) error = nil, want a refusal")
	}
	if err := scaffoldEndpoint("reports", "reports.unknown"); err == nil {
		t.Error("scaffoldEndpoint(unknown operation) error = nil, want a refusal")
	}
	if err := scaffoldEndpoint("reports", "reports.other"); err == nil {
		t.Error("scaffoldEndpoint(unallowlisted operation) error = nil, want a refusal")
	}
}

func TestScaffoldEndpointImplementsAllowlistedOperation(t *testing.T) {
	root := scaffoldFixture(t)
	if err := scaffoldFeature("reports"); err != nil {
		t.Fatalf("scaffoldFeature() error = %v", err)
	}
	if err := scaffoldEndpoint("reports", "reports.list"); err != nil {
		t.Fatalf("scaffoldEndpoint() error = %v", err)
	}

	allowlist := readFile(t, allowlistDefaultPath)
	if strings.Contains(allowlist, "reports.list") {
		t.Error("the allowlist still lists the implemented operation")
	}
	rootFile := readFile(t, "internal/features/reports/reports.go")
	for _, want := range []string{
		`ID: "reports.list"`,
		`Path: "/v1/reports"`,
		"Method: http.MethodGet",
		"func (h *handlers) handleList(",
		"type handlers struct {",
	} {
		if !strings.Contains(rootFile, want) {
			t.Errorf("feature root misses %q", want)
		}
	}
	for _, path := range []string{
		"internal/features/reports/app/list.go",
		"internal/features/reports/infra/list.go",
		"internal/features/reports/app/list_test.go",
		"db/queries/common/reports.sql",
		"cmd/api/contract_reports_list_test.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("%s missing: %v", path, err)
		}
	}
}

func TestResolveOperationReadsTheSnapshot(t *testing.T) {
	scaffoldFixture(t)

	spec, err := resolveOperation("reports.list")
	if err != nil {
		t.Fatalf("resolveOperation() error = %v", err)
	}
	if spec.method != "GET" || spec.path != "/v1/reports" {
		t.Errorf("spec = %+v, want GET /v1/reports", spec)
	}
	if spec.function != "handleList" {
		t.Errorf("function = %q, want handleList", spec.function)
	}
	if spec.slug() != "list" {
		t.Errorf("slug = %q, want list", spec.slug())
	}
	if _, err := resolveOperation("reports.other"); err == nil {
		t.Error("resolveOperation(unknown) error = nil, want a refusal")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	// #nosec G304 -- the test reads the fixture paths it just wrote.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRemoveFeatureUnwiresEverything(t *testing.T) {
	root := scaffoldFixture(t)
	if err := scaffoldFeature("reports"); err != nil {
		t.Fatalf("scaffoldFeature() error = %v", err)
	}
	if err := scaffoldEndpoint("reports", "reports.list"); err != nil {
		t.Fatalf("scaffoldEndpoint() error = %v", err)
	}

	if err := removeFeature("reports"); err != nil {
		t.Fatalf("removeFeature() error = %v", err)
	}

	server := readFile(t, serverPath)
	if strings.Contains(server, "features/reports") || strings.Contains(server, "reports.Register(") {
		t.Error("the composition root still wires the removed feature")
	}
	if !strings.Contains(server, featureRegistrationMarker) {
		t.Error("the marker must survive the removal so the next scaffold can wire")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/features/reports")); err == nil {
		t.Error("the feature directory survived the removal")
	}
	for _, path := range []string{
		"cmd/api/contract_reports_list_test.go",
		"db/queries/common/reports.sql",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			t.Errorf("%s survived the removal", path)
		}
	}
	if allowlist := readFile(t, allowlistDefaultPath); !strings.Contains(allowlist, "  - reports.list") {
		t.Error("the implemented operation did not return to the allowlist")
	}
}

func TestRemoveRefusesUnknownFeature(t *testing.T) {
	scaffoldFixture(t)
	if err := removeFeature("reports"); err == nil {
		t.Error("removeFeature(missing) error = nil, want a refusal")
	}
	if err := removeFeature("Reports"); err == nil {
		t.Error("removeFeature(bad name) error = nil, want a refusal")
	}
}

func TestRemoveKeepsFilesWhenNotWired(t *testing.T) {
	root := scaffoldFixture(t)
	if err := scaffoldFeature("reports"); err != nil {
		t.Fatalf("scaffoldFeature() error = %v", err)
	}
	// A feature someone deleted by hand must refuse rather than half-remove.
	if err := os.WriteFile(filepath.Join(root, serverPath), []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("rewrite server: %v", err)
	}
	if err := removeFeature("reports"); err == nil {
		t.Fatal("removeFeature() error = nil, want the unwired refusal")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/features/reports/reports.go")); err != nil {
		t.Errorf("the refusal must leave the feature in place: %v", err)
	}
}

func TestScaffoldTwoEndpointsOnOneFeature(t *testing.T) {
	scaffoldFixture(t)
	if err := scaffoldFeature("reports"); err != nil {
		t.Fatalf("scaffoldFeature() error = %v", err)
	}
	for _, operation := range []string{"reports.list", "reports.get"} {
		if err := scaffoldEndpoint("reports", operation); err != nil {
			t.Fatalf("scaffoldEndpoint(%s) error = %v", operation, err)
		}
	}

	root := readFile(t, "internal/features/reports/reports.go")
	if got := strings.Count(root, "type handlers struct"); got != 1 {
		t.Errorf("handlers structs = %d, want exactly one", got)
	}
	if got := strings.Count(root, `"net/http"`); got != 1 {
		t.Errorf("net/http imports = %d, want exactly one", got)
	}
	for _, want := range []string{`ID: "reports.list"`, `ID: "reports.get"`, "func (h *handlers) handleList(", "func (h *handlers) handleGet("} {
		if !strings.Contains(root, want) {
			t.Errorf("feature root misses %q", want)
		}
	}
	// Distinct operations must not collide on file names.
	for _, path := range []string{
		"internal/features/reports/app/list.go",
		"internal/features/reports/app/get.go",
		"internal/features/reports/infra/get.go",
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s missing: %v", path, err)
		}
	}
}
