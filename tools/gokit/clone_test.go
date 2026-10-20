package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cloneFixture runs the clone from the kit's root, which is where the command
// expects to stand, and restores the working directory afterwards.
func cloneFixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	// The generated project's commit needs an identity; CI has none configured.
	for key, value := range map[string]string{
		"GIT_AUTHOR_NAME":     "clone test",
		"GIT_AUTHOR_EMAIL":    "clone-test@example.invalid",
		"GIT_COMMITTER_NAME":  "clone test",
		"GIT_COMMITTER_EMAIL": "clone-test@example.invalid",
	} {
		t.Setenv(key, value)
	}
	return root
}

func TestCloneGeneratesAProject(t *testing.T) {
	root := cloneFixture(t)
	target := filepath.Join(t.TempDir(), "newproj")
	if err := runClone(context.Background(), target, "newproj", "example.com/newproj", false); err != nil {
		t.Fatalf("runClone() error = %v", err)
	}

	for _, check := range []struct {
		path string
		want string
		miss string
	}{
		{"go.mod", "module example.com/newproj", ""},
		{"cmd/api/main.go", "internal/platform/config", ""},
		{"README.md", "Generated from go-core-kit", ""},
		{"internal/platform/dblane/lane_sqlite.go", "example.com/newproj/internal/platform/config", "go-core-kit"},
	} {
		data, err := os.ReadFile(filepath.Join(target, check.path)) //nolint:gosec // G304: the clone's own file.
		if err != nil {
			t.Fatalf("read %s: %v", check.path, err)
		}
		if !strings.Contains(string(data), check.want) {
			t.Errorf("%s does not contain %q", check.path, check.want)
		}
		if check.miss != "" && strings.Contains(string(data), check.miss) {
			t.Errorf("%s still contains %q", check.path, check.miss)
		}
	}
	for _, gone := range []string{"docs/exec-plans", "_WIP", ".git"} {
		if _, err := os.Stat(filepath.Join(target, gone)); err == nil && gone != ".git" {
			t.Errorf("%s survived the clone", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatalf("the clone has no fresh git history: %v", err)
	}
	log := exec.CommandContext(t.Context(), "git", "-C", target, "log", "--oneline") //nolint:gosec // G204: fixed command.
	if output, err := log.CombinedOutput(); err != nil || strings.Count(strings.TrimSpace(string(output)), "\n") != 0 {
		t.Errorf("clone history = %q (%v), want exactly one commit", output, err)
	}
	if !strings.Contains(readString(t, filepath.Join(target, "docs", "README.md")), "docs/adr") {
		t.Error("the docs index lost its surviving sections")
	}
	if strings.Contains(readString(t, filepath.Join(target, "docs", "README.md")), "Execution plans") {
		t.Error("the docs index still points at stripped plans")
	}
	_ = root
}

func TestCloneDryRunWritesNothing(t *testing.T) {
	cloneFixture(t)
	target := filepath.Join(t.TempDir(), "dryrun")
	if err := runClone(context.Background(), target, "dryrun", "example.com/dryrun", true); err != nil {
		t.Fatalf("runClone(dry-run) error = %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("the dry run created the target")
	}
}

func TestCloneRefusesOverlapAndNonEmptyTarget(t *testing.T) {
	root := cloneFixture(t)
	if err := runClone(context.Background(), filepath.Join(root, "inside"), "", "example.com/x", true); err == nil {
		t.Error("cloning inside the kit was allowed")
	}
	busy := t.TempDir()
	if err := os.WriteFile(filepath.Join(busy, "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := runClone(context.Background(), busy, "", "example.com/x", true); err == nil {
		t.Error("cloning into a non-empty directory was allowed")
	}
	if err := runClone(context.Background(), filepath.Join(t.TempDir(), "m"), "", "nomodule", true); err == nil {
		t.Error("cloning with a malformed module path was allowed")
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // G304: the clone's own file.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
