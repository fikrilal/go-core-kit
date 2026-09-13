package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultStepBudget = 2 * time.Minute

type step struct {
	id     string
	budget time.Duration
	run    func(ctx context.Context) error
}

func execStep(id string, name string, args ...string) step {
	return step{
		id:     id,
		budget: defaultStepBudget,
		run: func(ctx context.Context) error {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // executable names are fixed in the profile registry
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
			}
			return nil
		},
	}
}

func knowledgeStep() step {
	return step{
		id:     "knowledge",
		budget: 30 * time.Second,
		run: func(context.Context) error {
			return runKnowledgeCheck()
		},
	}
}

func contractStep() step {
	return step{
		id:     "contract",
		budget: 30 * time.Second,
		run: func(context.Context) error {
			return runContractCheck()
		},
	}
}

func runDriftCheck(ctx context.Context) error {
	if err := generateDB(); err != nil {
		return err
	}
	if err := runContractGenerate(ctx); err != nil {
		return err
	}
	status, err := generatedDrift(ctx, genDir, contractGeneratedPath)
	if err != nil {
		return err
	}
	if status != "" {
		if strings.Contains(status, "?? ") {
			return fmt.Errorf("generated code is not committed — commit the generated artifacts:\n%s", status)
		}
		return fmt.Errorf("generated code is out of date — run make generate and commit:\n%s", status)
	}
	fmt.Println("drift: generated artifacts are committed and current")
	return nil
}

func driftStep() step {
	return step{
		id:     "drift",
		budget: 2 * time.Minute,
		run:    runDriftCheck,
	}
}

func contractLintStep() step {
	return execStep("contract-lint", "vacuum", "lint", "--no-banner", "--fail-severity", "error", "--ruleset", "contracts/vacuum.yaml", "contracts/backend.openapi.yaml")
}

func gatesHonestyStep() step {
	return step{
		id:     "gates-honesty",
		budget: 2 * time.Minute,
		run:    runGatesHonesty,
	}
}

func fastSteps() []step {
	return []step{
		knowledgeStep(),
		contractStep(),
		execStep("fmt-check", "golangci-lint", "fmt", "--diff"),
		execStep("vet", "go", "vet", "./..."),
		execStep("lint", "golangci-lint", "run"),
		execStep("test", "go", "test", "./..."),
	}
}

func fullSteps() []step {
	return append(fastSteps(),
		execStep("build", "go", "build", "./..."),
		execStep("build-pg", "go", "build", "-tags", "pg", "./..."),
		driftStep(),
		contractLintStep(),
		gatesHonestyStep(),
	)
}

func ciSteps() []step {
	// The runtime lane joins here in phase 7 (Postgres matrix).
	return fullSteps()
}

var profiles = map[string][]step{
	"fast": fastSteps(),
	"full": fullSteps(),
	"ci":   ciSteps(),
}

func runProfile(ctx context.Context, name string) error {
	steps, ok := profiles[name]
	if !ok {
		return usagef("verify: unknown profile %q (want %s)", name, strings.Join(profileNames(), ", "))
	}
	return runSteps(ctx, name, steps)
}

func runSteps(ctx context.Context, name string, steps []step) error {
	fmt.Printf("verify: %s profile\n", name)
	start := time.Now()
	for _, s := range steps {
		if err := runStep(ctx, s); err != nil {
			return fmt.Errorf("verify: %s profile failed at %q: %w", name, s.id, err)
		}
	}
	fmt.Printf("verify: %s profile passed (%s)\n", name, time.Since(start).Round(time.Millisecond))
	return nil
}

func runStep(ctx context.Context, s step) error {
	fmt.Printf("==> %s\n", s.id)
	start := time.Now()
	err := s.run(ctx)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("FAIL %s (%s)\n", s.id, elapsed.Round(time.Millisecond))
		return err
	}
	fmt.Printf("ok %s (%s)\n", s.id, elapsed.Round(time.Millisecond))
	if s.budget > 0 && elapsed > s.budget {
		fmt.Printf("warning: %s took %s (advisory budget %s)\n", s.id, elapsed.Round(time.Millisecond), s.budget)
	}
	return nil
}

func commandOutput(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // executable names are fixed by callers
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// generatedDrift reports any committed-state difference under paths —
// modifications, deletions, and untracked generated files.
func generatedDrift(ctx context.Context, paths ...string) (string, error) {
	args := append([]string{"status", "--porcelain", "--"}, paths...)
	return commandOutput(ctx, "git", args...)
}
