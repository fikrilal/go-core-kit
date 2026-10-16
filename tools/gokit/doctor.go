package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	makefilePinPattern = regexp.MustCompile(`(?m)^(GOLANGCI_LINT_VERSION|SQLC_VERSION|OAPI_CODEGEN_VERSION|VACUUM_VERSION|GOOSE_VERSION) \?= (\S+)$`)
	goDirectivePattern = regexp.MustCompile(`(?m)^go (\S+)$`)
	goVersionPattern   = regexp.MustCompile(`\bgo(\d+\.\d+(?:\.\d+)?)\b`)
	golangciPattern    = regexp.MustCompile(`version (\S+) built`)
	versionOnlyPattern = regexp.MustCompile(`(?m)^v?(\d+\.\d+\.\d+)$`)
	goosePattern       = regexp.MustCompile(`goose version: v?(\S+)`)
)

type doctorReport struct {
	passes   int
	warnings int
	failures int
}

func (r *doctorReport) pass(message string) {
	r.passes++
	fmt.Println("ok    " + message)
}

func (r *doctorReport) warn(message string) {
	r.warnings++
	fmt.Println("warn  " + message)
}

func (r *doctorReport) fail(message string) {
	r.failures++
	fmt.Println("error " + message)
}

func runDoctor(ctx context.Context) error {
	report := &doctorReport{}

	required := []string{"go.mod", "Makefile", "sqlc.yaml", "AGENTS.md", "docs/README.md"}
	var missing []string
	for _, file := range required {
		if _, err := os.Stat(file); err != nil {
			missing = append(missing, file)
		}
	}
	if len(missing) > 0 {
		report.fail("missing repository files (run from the repository root): " + strings.Join(missing, ", "))
	} else {
		report.pass("repository files present: " + strings.Join(required, ", "))
	}

	pins := readPins(report)
	checkGoVersion(ctx, report)
	checkTool(ctx, report, "golangci-lint", []string{"version"}, golangciPattern, pins["GOLANGCI_LINT_VERSION"])
	checkTool(ctx, report, "sqlc", []string{"version"}, versionOnlyPattern, pins["SQLC_VERSION"])
	checkTool(ctx, report, "oapi-codegen", []string{"--version"}, versionOnlyPattern, pins["OAPI_CODEGEN_VERSION"])
	checkTool(ctx, report, "vacuum", []string{"version"}, versionOnlyPattern, pins["VACUUM_VERSION"])
	// The goose binary is an operator tool: migrations run in process, so a
	// missing CLI warns rather than failing the check.
	checkOptionalTool(ctx, report, "goose", []string{"--version"}, goosePattern, pins["GOOSE_VERSION"])

	if _, err := commandOutput(ctx, "git", "rev-parse", "--show-toplevel"); err != nil {
		report.warn("not a git work tree — the review loop and drift checks need one")
	} else {
		report.pass("git work tree")
	}

	fmt.Printf("doctor: %d ok, %d warning(s), %d error(s)\n", report.passes, report.warnings, report.failures)
	if report.failures > 0 {
		return fmt.Errorf("doctor: %d error(s)", report.failures)
	}
	return nil
}

func readPins(report *doctorReport) map[string]string {
	data, err := os.ReadFile("Makefile")
	if err != nil {
		report.warn("Makefile unreadable: " + err.Error())
		return nil
	}
	pins := parsePins(string(data))
	if pins["GOLANGCI_LINT_VERSION"] == "" || pins["SQLC_VERSION"] == "" || pins["OAPI_CODEGEN_VERSION"] == "" || pins["VACUUM_VERSION"] == "" || pins["GOOSE_VERSION"] == "" {
		report.warn("Makefile pins not found (GOLANGCI_LINT_VERSION, SQLC_VERSION, OAPI_CODEGEN_VERSION, VACUUM_VERSION, GOOSE_VERSION)")
	}
	return pins
}

func parsePins(content string) map[string]string {
	pins := map[string]string{}
	for _, match := range makefilePinPattern.FindAllStringSubmatch(content, -1) {
		pins[match[1]] = match[2]
	}
	return pins
}

func checkGoVersion(ctx context.Context, report *doctorReport) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		report.fail("go.mod unreadable: " + err.Error())
		return
	}
	requiredMatch := goDirectivePattern.FindStringSubmatch(string(data))
	if requiredMatch == nil {
		report.warn("go.mod has no go directive")
		return
	}
	required := requiredMatch[1]

	output, err := commandOutput(ctx, "go", "version")
	if err != nil {
		report.fail("go not runnable (" + err.Error() + ")")
		return
	}
	installedMatch := goVersionPattern.FindStringSubmatch(output)
	if installedMatch == nil {
		report.warn("go: unrecognized version output " + firstLine(output))
		return
	}
	installed := installedMatch[1]
	if compareVersions(installed, required) < 0 {
		report.fail(fmt.Sprintf("go %s installed, go.mod requires %s", installed, required))
		return
	}
	report.pass(fmt.Sprintf("go %s (go.mod requires %s)", installed, required))
}

func checkTool(ctx context.Context, report *doctorReport, name string, args []string, pattern *regexp.Regexp, pinned string) {
	output, err := commandOutput(ctx, name, args...)
	if err != nil {
		report.fail(fmt.Sprintf("%s not runnable — run `make tools` (%v)", name, err))
		return
	}
	match := pattern.FindStringSubmatch(output)
	if match == nil {
		report.warn(fmt.Sprintf("%s: unrecognized version output %q", name, firstLine(output)))
		return
	}
	installed := match[1]
	if pinned == "" {
		report.pass(name + " " + installed)
		return
	}
	if strings.TrimPrefix(pinned, "v") != installed {
		report.warn(fmt.Sprintf("%s %s installed, Makefile pins %s — run `make tools`", name, installed, pinned))
		return
	}
	report.pass(fmt.Sprintf("%s %s (Makefile pins %s)", name, installed, pinned))
}

// checkOptionalTool reports a tool that operational work uses but the build does
// not require.
func checkOptionalTool(ctx context.Context, report *doctorReport, name string, args []string, pattern *regexp.Regexp, pinned string) {
	output, err := commandOutput(ctx, name, args...)
	if err != nil {
		report.warn(fmt.Sprintf("%s not installed — run `make tools` to use it for manual migrations", name))
		return
	}
	match := pattern.FindStringSubmatch(output)
	if match == nil {
		report.warn(fmt.Sprintf("%s: unrecognized version output %q", name, firstLine(output)))
		return
	}
	if pinned != "" && strings.TrimPrefix(pinned, "v") != match[1] {
		report.warn(fmt.Sprintf("%s %s installed, Makefile pins %s — run `make tools`", name, match[1], pinned))
		return
	}
	report.pass(fmt.Sprintf("%s %s (Makefile pins %s)", name, match[1], pinned))
}

func compareVersions(a, b string) int {
	partsA := strings.Split(strings.TrimPrefix(a, "v"), ".")
	partsB := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(partsA) || i < len(partsB); i++ {
		valueA, valueB := 0, 0
		if i < len(partsA) {
			valueA, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			valueB, _ = strconv.Atoi(partsB[i])
		}
		if valueA != valueB {
			if valueA < valueB {
				return -1
			}
			return 1
		}
	}
	return 0
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}
