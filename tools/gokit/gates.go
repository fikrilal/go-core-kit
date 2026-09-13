package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	honestyViolationRoot = "internal/features"
	honestyViolationDir  = honestyViolationRoot + "/violation"
	honestyViolationFile = "violation.go"

	markerDigestMismatch    = "snapshot digest does not match provenance"
	markerCoverageMissing   = "missing from the allowlist and not implemented"
	markerDriverConfinement = "features must not import database drivers"
)

const honestyViolationSource = `// Package violation is a gate-honesty fixture.
package violation

import (
	_ "modernc.org/sqlite" // gate-honesty fixture imports a confined driver on purpose
)
`

const honestyFakeOperation = `  /v1/gates/fake:
    get:
      description: Injected by the gate-honesty coverage check.
      operationId: gates.honesty.fake
      parameters: []
      responses:
        "200":
          description: ""
      summary: Fake operation
      tags:
        - Health
      x-error-codes:
        - INTERNAL
`

type honestyScenario struct {
	name   string
	verify func(ctx context.Context) error
}

func runGatesHonesty(ctx context.Context) error {
	scenarios := []honestyScenario{
		{name: "stale snapshot fails the digest gate", verify: verifyDigestHonesty},
		{name: "undeclared operation fails the coverage gate", verify: verifyCoverageHonesty},
		{name: "forbidden import fails the boundary gate", verify: verifyBoundaryHonesty},
	}
	failed := 0
	for _, scenario := range scenarios {
		fmt.Printf("==> %s\n", scenario.name)
		if err := scenario.verify(ctx); err != nil {
			failed++
			fmt.Printf("FAIL %s: %v\n", scenario.name, err)
			continue
		}
		fmt.Printf("ok %s\n", scenario.name)
	}
	if failed > 0 {
		return fmt.Errorf("gates honesty: %d of %d scenarios failed", failed, len(scenarios))
	}
	fmt.Println("gates honesty: every probed gate failed as designed")
	return nil
}

func verifyDigestHonesty(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "gokit-gates-digest-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	snapshot, err := os.ReadFile(snapshotDefaultPath)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	tampered := filepath.Join(dir, "backend.openapi.yaml")
	if err := os.WriteFile(tampered, append(snapshot, []byte("# tampered by gate honesty\n")...), 0o600); err != nil { //nolint:gosec // temp-dir fixture
		return fmt.Errorf("write tampered snapshot: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate gokit binary: %w", err)
	}
	return expectGateFailure(ctx, markerDigestMismatch, withEnv("CONTRACT_SNAPSHOT_PATH="+tampered), self, "contract", "check")
}

func verifyCoverageHonesty(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "gokit-gates-coverage-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	snapshot, err := os.ReadFile(snapshotDefaultPath)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	doctored := strings.Replace(string(snapshot), "\ninfo:", "\n"+honestyFakeOperation+"info:", 1)
	if doctored == string(snapshot) {
		return fmt.Errorf("coverage injection anchor not found in the snapshot")
	}
	tampered := filepath.Join(dir, "backend.openapi.yaml")
	if err := os.WriteFile(tampered, []byte(doctored), 0o600); err != nil { //nolint:gosec // temp-dir fixture
		return fmt.Errorf("write doctored snapshot: %w", err)
	}
	return expectGateFailure(ctx, markerCoverageMissing,
		withEnv("CONTRACT_SNAPSHOT_PATH="+tampered),
		"go", "test", "./cmd/api/...", "-run", "TestContractCoverage", "-count=1")
}

func verifyBoundaryHonesty(ctx context.Context) error {
	if err := os.MkdirAll(honestyViolationDir, 0o755); err != nil { //nolint:gosec // repository fixture directory
		return fmt.Errorf("create violation directory: %w", err)
	}
	defer func() {
		_ = os.Remove(filepath.Join(honestyViolationDir, honestyViolationFile))
		_ = os.Remove(honestyViolationDir)
		_ = os.Remove(honestyViolationRoot)
	}()
	if err := os.WriteFile(filepath.Join(honestyViolationDir, honestyViolationFile), []byte(honestyViolationSource), 0o600); err != nil {
		return fmt.Errorf("write violation fixture: %w", err)
	}
	return expectGateFailure(ctx, markerDriverConfinement, os.Environ(), "golangci-lint", "run", "./internal/features/violation/...")
}

func expectGateFailure(ctx context.Context, marker string, env []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // gate commands are fixed by the harness
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err == nil {
		return fmt.Errorf("gate %q unexpectedly succeeded; it may be ineffective", strings.Join(append([]string{name}, args...), " "))
	}
	if !strings.Contains(string(output), marker) {
		return fmt.Errorf("gate %q failed without the expected marker %q:\n%s", name, marker, output)
	}
	return nil
}

func withEnv(extra ...string) []string {
	return append(os.Environ(), extra...)
}
