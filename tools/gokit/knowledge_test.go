package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseEnvKeys(t *testing.T) {
	content := "# comment\n\nAPP_ENV=dev\nHTTP_PORT=8080\n# REDIS_URL=skipped\nlower=1\n"
	got := parseEnvKeys(content)
	want := []string{"APP_ENV", "HTTP_PORT"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseEnvKeys() = %v, want %v", got, want)
	}
}

func TestParseProjectMap(t *testing.T) {
	markdown := "intro\n\n```text\ncmd/api/      bootstrap\ninternal/x/   thing\n```\n\nmore"
	entries, err := parseProjectMap(markdown)
	if err != nil {
		t.Fatalf("parseProjectMap() error = %v", err)
	}
	want := []string{"cmd/api/", "internal/x/"}
	if !slices.Equal(entries, want) {
		t.Fatalf("parseProjectMap() = %v, want %v", entries, want)
	}
}

func TestParseProjectMapMissingBlock(t *testing.T) {
	if _, err := parseProjectMap("no fences here"); err == nil {
		t.Fatal("parseProjectMap() = nil, want error")
	}
}

func TestLinkIssues(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	if err := os.WriteFile(good, []byte("x\n"), 0o600); err != nil {
		t.Fatalf("write good.md: %v", err)
	}
	source := filepath.Join(dir, "source.md")
	content := "[ok](./good.md)\n[missing](./bad.md)\n[web](https://example.com)\n[anchor](#top)\n"
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatalf("write source.md: %v", err)
	}

	issues, err := linkIssues(source)
	if err != nil {
		t.Fatalf("linkIssues() error = %v", err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0], "./bad.md") {
		t.Fatalf("linkIssues() = %v, want one issue for ./bad.md", issues)
	}
}

func TestPlannedPathsLeaveShippedFeaturesChecked(t *testing.T) {
	if slices.Contains(plannedPaths, "internal/features/") {
		t.Fatal("internal/features/ is still exempt after the feature shipped")
	}
	for _, path := range []string{"cmd/worker/", "internal/shared/"} {
		if !slices.Contains(plannedPaths, path) {
			t.Errorf("plannedPaths missing %s", path)
		}
	}
}

func TestKnowledgeCheckOnRepository(t *testing.T) {
	chdirRepoRoot(t)
	if err := runKnowledgeCheck(); err != nil {
		t.Fatalf("runKnowledgeCheck() error = %v", err)
	}
}

func TestEnvExampleMatchesSchema(t *testing.T) {
	chdirRepoRoot(t)
	issues, err := checkEnvExample()
	if err != nil {
		t.Fatalf("checkEnvExample() error = %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("env.example drift: %v", issues)
	}
}

func chdirRepoRoot(t *testing.T) {
	t.Helper()
	t.Chdir(filepath.Join("..", ".."))
}
