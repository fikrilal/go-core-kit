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
	honestyFeaturesRoot = "internal/features"
	honestyPlatformRoot = "internal/platform"

	markerDigestMismatch     = "snapshot digest does not match provenance"
	markerCoverageMissing    = "missing from the allowlist and not implemented"
	markerDriverConfinement  = "features must not import database drivers"
	markerRawJSON            = "use httpx decode/respond helpers instead of raw JSON in features"
	markerClockInjection     = "inject a clock"
	markerPlatformNoFeatures = "platform must not depend on features"
)

const driversViolationSource = `// Package drivers is a gate-honesty fixture.
package drivers

import (
	_ "modernc.org/sqlite" // gate-honesty fixture imports a confined driver on purpose
)
`

const rawJSONViolationSource = `// Package rawjson is a gate-honesty fixture.
package rawjson

import (
	"encoding/json"
)

// Encode is a gate-honesty fixture.
var Encode = json.Marshal
`

const clockViolationSource = `// Package clock is a gate-honesty fixture.
package clock

import (
	"time"
)

// Now is a gate-honesty fixture.
func Now() time.Time {
	return time.Now()
}
`

const platformViolationSource = `// Package violation is a gate-honesty fixture.
package violation

import (
	"github.com/fikrilal/go-core-kit/internal/features/auth"
)

// Gate is a gate-honesty fixture.
var Gate = auth.Deps{}
`

// violationFixture is a boundary-rule probe: a temporary package that must
// fail golangci-lint with the expected marker.
type violationFixture struct {
	name    string
	file    string
	source  string
	pattern string
	marker  string
	dirs    []string
	root    string
}

var violationFixtures = []violationFixture{
	{
		name:    "confined driver import fails the boundary gate",
		file:    "internal/features/violation/drivers/violation.go",
		source:  driversViolationSource,
		pattern: "./internal/features/violation/drivers/...",
		marker:  markerDriverConfinement,
		dirs:    []string{"internal/features/violation/drivers", "internal/features/violation"},
		root:    honestyFeaturesRoot,
	},
	{
		name:    "raw JSON import fails the boundary gate",
		file:    "internal/features/violation/rawjson/violation.go",
		source:  rawJSONViolationSource,
		pattern: "./internal/features/violation/rawjson/...",
		marker:  markerRawJSON,
		dirs:    []string{"internal/features/violation/rawjson", "internal/features/violation"},
		root:    honestyFeaturesRoot,
	},
	{
		name:    "time.Now call fails the boundary gate",
		file:    "internal/features/violation/clock/violation.go",
		source:  clockViolationSource,
		pattern: "./internal/features/violation/clock/...",
		marker:  markerClockInjection,
		dirs:    []string{"internal/features/violation/clock", "internal/features/violation"},
		root:    honestyFeaturesRoot,
	},
	{
		name:    "platform importing features fails the boundary gate",
		file:    "internal/platform/violation/violation.go",
		source:  platformViolationSource,
		pattern: "./internal/platform/violation/...",
		marker:  markerPlatformNoFeatures,
		dirs:    []string{"internal/platform/violation"},
		root:    honestyPlatformRoot,
	},
}

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
	}
	for _, fixture := range violationFixtures {
		scenarios = append(scenarios, honestyScenario{
			name: fixture.name,
			verify: func(ctx context.Context) error {
				return runViolationFixture(ctx, fixture)
			},
		})
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
	// The fake path is inserted before the root info: key.
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

func runViolationFixture(ctx context.Context, fixture violationFixture) error {
	_, statErr := os.Stat(fixture.root)
	rootExisted := statErr == nil

	if err := os.MkdirAll(filepath.Dir(fixture.file), 0o755); err != nil { //nolint:gosec // repository fixture directory
		return fmt.Errorf("create violation directory: %w", err)
	}
	defer func() {
		_ = os.Remove(fixture.file)
		for _, dir := range fixture.dirs {
			_ = os.Remove(dir)
		}
		if !rootExisted {
			_ = os.Remove(fixture.root)
		}
	}()
	if err := os.WriteFile(fixture.file, []byte(fixture.source), 0o600); err != nil {
		return fmt.Errorf("write violation fixture: %w", err)
	}
	return expectGateFailure(ctx, fixture.marker, os.Environ(), "golangci-lint", "run", fixture.pattern)
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
