package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// cloneSourceModule is the module path a generated project starts from.
const cloneSourceModule = "github.com/fikrilal/go-core-kit"

// cloneStripPaths are removed from the generated project. The roadmap, active and
// completed plans are the kit's own records; a new project starts without them.
var cloneStripPaths = []string{
	".git",
	"_WIP",
	"data",
	".env",
	".cache",
	"worker",
	"api",
	"gokit",
	"docs/exec-plans",
}

// cloneTextFiles are the files whose prose names the kit rather than the module, so
// the project's own name replaces it.
var cloneTextFiles = []string{"README.md", "AGENTS.md", "env.example"}

type cloneCommand struct {
	dir    string
	name   string
	module string
	dryRun bool
}

func (c cloneCommand) execute(ctx context.Context) error {
	return runClone(ctx, c.dir, c.name, c.module, c.dryRun)
}

// runClone writes a new project at dir: the kit's files, the kit's records removed,
// the module and its imports renamed, and one commit of its own.
func runClone(ctx context.Context, dir, name, module string, dryRun bool) error {
	if !strings.Contains(module, "/") {
		return usagef("clone: --module must be a Go module path like github.com/you/project")
	}
	source, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("clone: working directory: %w", err)
	}
	if _, err := os.Stat(filepath.Join(source, "go.mod")); err != nil {
		return usagef("clone: run from the kit's repository root")
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	target, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("clone: resolve target: %w", err)
	}
	if inside(target, source) || inside(source, target) {
		return usagef("clone: target %s overlaps the kit it is generated from", target)
	}
	if entries, err := os.ReadDir(target); err == nil && len(entries) > 0 {
		return usagef("clone: %s is not empty", target)
	}

	plan, err := clonePlan(source)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Printf("dry-run: would generate %s (module %s) from %s\n", target, module, source)
		for _, path := range cloneStripPaths {
			fmt.Println("  strip   " + path)
		}
		for _, path := range plan {
			fmt.Println("  rewrite " + path)
		}
		fmt.Println("dry-run: nothing was written")
		return nil
	}

	if err := copyTree(source, target); err != nil {
		return err
	}
	if err := rewriteClone(target, module, name, plan); err != nil {
		return err
	}
	if err := initCloneHistory(ctx, target); err != nil {
		return err
	}
	fmt.Printf("clone: generated %s as module %s\n", target, module)
	fmt.Println("next: cd " + dir + " && make verify")
	return nil
}

// clonePlan lists the files the module rewrite will touch, so a dry run can print it.
func clonePlan(source string) ([]string, error) {
	var plan []string
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return fmt.Errorf("clone: %w", relErr)
		}
		if entry.IsDir() {
			if shouldStrip(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldStrip(relative) {
			return nil
		}
		inDocs := strings.HasPrefix(relative, "docs"+string(filepath.Separator)) && filepath.Ext(relative) == ".md"
		if filepath.Ext(path) == ".go" || relative == "go.mod" || isCloneTextFile(relative) || inDocs {
			plan = append(plan, relative)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clone: plan: %w", err)
	}
	return plan, nil
}

// copyTree copies the kit's files, leaving the stripped records and build outputs.
func copyTree(source, target string) error {
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return fmt.Errorf("clone: %w", relErr)
		}
		if shouldStrip(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			if mkErr := os.MkdirAll(destination, 0o750); mkErr != nil {
				return fmt.Errorf("clone: create %s: %w", destination, mkErr)
			}
			return nil
		}
		return copyFile(path, destination)
	})
	if err != nil {
		return fmt.Errorf("clone: copy tree: %w", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source) //nolint:gosec // G304: the walk stays inside the kit.
	if err != nil {
		return fmt.Errorf("clone: read %s: %w", source, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: path comes from the clone walk.
	if err != nil {
		return fmt.Errorf("clone: write %s: %w", destination, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("clone: copy %s: %w", source, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("clone: close %s: %w", destination, err)
	}
	return nil
}

// rewriteClone renames the module in code and the kit in prose, and drops the docs
// index entries whose files were stripped.
func rewriteClone(target, module, name string, plan []string) error {
	for _, relative := range plan {
		path := filepath.Join(target, relative)

		data, err := os.ReadFile(path) //nolint:gosec // G304: paths come from the clone walk or the plan.
		if err != nil {
			return fmt.Errorf("clone: read %s: %w", relative, err)
		}
		content := strings.ReplaceAll(string(data), cloneSourceModule, module)
		if isCloneTextFile(relative) || strings.HasPrefix(relative, "docs"+string(filepath.Separator)) {
			content = strings.ReplaceAll(content, "go-core-kit", name)
		}
		if relative == filepath.Join("docs", "README.md") {
			content = stripExecPlanIndex(content)
		}
		content = unlinkStrippedDocs(content)
		if strings.HasPrefix(relative, "docs"+string(filepath.Separator)) {
			content = unlinkExecPlans(content)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("clone: write %s: %w", relative, err)
		}
	}
	banner := fmt.Sprintf("> Generated from go-core-kit (%s) on %s. The provenance and the kit's\n> decisions are recorded in the repository you copied from.\n\n", revision(), today())
	return prependBanner(filepath.Join(target, "README.md"), banner)
}

func prependBanner(path, banner string) error {
	data, err := os.ReadFile(path) //nolint:gosec // G304: paths come from the clone walk or the plan.
	if err != nil {
		return fmt.Errorf("clone: read README: %w", err)
	}

	if err := os.WriteFile(path, append([]byte(banner), data...), 0o600); err != nil { //nolint:gosec // G304: paths come from the clone walk or the plan.
		return fmt.Errorf("clone: write README: %w", err)
	}
	return nil
}

// unlinkStrippedDocs turns links to the kit's own records into plain text and drops
// the navigation lines that named them, so a generated project has no dangling
// targets once those files are gone.
func unlinkStrippedDocs(content string) string {
	link := regexp.MustCompile(`\[([^\]]+)\]\(([^)]*(?:exec-plans|_WIP)[^)]*)\)`)
	content = link.ReplaceAllString(content, "$1")
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "_WIP") || trimmed == "- Proposals (historical, not source of truth)" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// unlinkExecPlans turns links to the kit's own plan records into plain text, so the
// generated project's markdown has no dangling targets once those files are gone.
func unlinkExecPlans(content string) string {
	link := regexp.MustCompile(`\[([^\]]+)\]\(([^)]*exec-plans[^)]*)\)`)
	return link.ReplaceAllString(content, "$1")
}

// stripExecPlanIndex removes the navigation entries whose files the clone strips:
// a line filter rather than a regex, so indentation changes cannot slip past it.
func stripExecPlanIndex(content string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "exec-plans") || strings.TrimSpace(line) == "- Execution plans" {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// initCloneHistory gives the generated project one commit and no inherited history.
func initCloneHistory(ctx context.Context, target string) error {
	steps := [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"commit", "-q", "-m", "chore: generate " + filepath.Base(target) + " from go-core-kit"},
	}
	for _, args := range steps {
		//nolint:gosec // G204: the git arguments are fixed above.
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = target
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("clone: git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

// shouldStrip reports whether a relative path is one of the kit's own records.
func shouldStrip(relative string) bool {
	if relative == "." {
		return false
	}
	for _, stripped := range cloneStripPaths {
		if relative == stripped || strings.HasPrefix(relative, stripped+string(filepath.Separator)) {
			return true
		}
	}
	return strings.HasSuffix(relative, ".db") || strings.HasSuffix(relative, ".db-wal") || strings.HasSuffix(relative, ".db-shm")
}

func isCloneTextFile(relative string) bool {
	for _, name := range cloneTextFiles {
		if relative == name {
			return true
		}
	}
	return false
}

// inside reports whether path is target or sits beneath it.
func inside(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || !strings.HasPrefix(relative, "..")
}

func revision() string {
	ctx := context.Background()
	output, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func today() string {
	ctx := context.Background()
	output, err := exec.CommandContext(ctx, "date", "-u", "+%Y-%m-%d").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}
