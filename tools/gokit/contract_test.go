package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSnapshot(t *testing.T) {
	valid := []byte("openapi: 3.0.0\npaths:\n")
	if err := validateSnapshot(valid); err != nil {
		t.Fatalf("validateSnapshot(valid) error = %v", err)
	}
	cases := [][]byte{
		[]byte("swagger: '2.0'\npaths:\n"),
		[]byte("openapi: 3.0.0\ncomponents:\n"),
		[]byte(""),
	}
	for _, contents := range cases {
		if err := validateSnapshot(contents); err == nil {
			t.Errorf("validateSnapshot(%q) = nil, want error", contents)
		}
	}
}

func TestDeriveProvenance(t *testing.T) {
	dir := t.TempDir()
	runGitInTest(t, dir, "init")
	source := filepath.Join(dir, "openapi.yaml")
	contents := []byte("openapi: 3.0.0\npaths:\n")
	if err := os.WriteFile(source, contents, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runGitInTest(t, dir, "add", "openapi.yaml")
	runGitInTest(t, dir, "-c", "user.email=gokit@test", "-c", "user.name=gokit", "commit", "-m", "pin")

	pinned, err := deriveProvenance(context.Background(), source, contents)
	if err != nil {
		t.Fatalf("deriveProvenance() error = %v", err)
	}
	commit := gitOutputInTest(t, dir, "rev-parse", "HEAD")
	if pinned.SourceCommit != commit {
		t.Errorf("SourceCommit = %q, want %q", pinned.SourceCommit, commit)
	}
	if pinned.SourcePath != "openapi.yaml" {
		t.Errorf("SourcePath = %q, want openapi.yaml", pinned.SourcePath)
	}
	if pinned.Sha256 != sha256Hex(contents) {
		t.Errorf("Sha256 = %q, want %q", pinned.Sha256, sha256Hex(contents))
	}
	if pinned.SchemaVersion != 1 || pinned.SourceRepository == "" {
		t.Errorf("provenance = %+v, want schemaVersion 1 and a repository", pinned)
	}

	if err := os.WriteFile(source, []byte("openapi: 3.0.0\npaths:\n# dirty\n"), 0o600); err != nil {
		t.Fatalf("dirty source: %v", err)
	}
	if _, err := deriveProvenance(context.Background(), source, contents); err == nil {
		t.Fatal("deriveProvenance() = nil on dirty source, want error")
	}
}

func TestDeriveProvenanceRefusesUntracked(t *testing.T) {
	dir := t.TempDir()
	runGitInTest(t, dir, "init")
	source := filepath.Join(dir, "openapi.yaml")
	contents := []byte("openapi: 3.0.0\npaths:\n")
	if err := os.WriteFile(source, contents, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if _, err := deriveProvenance(context.Background(), source, contents); err == nil {
		t.Fatal("deriveProvenance() = nil on untracked source, want error")
	}
}

func TestContractCheckOnPinnedTree(t *testing.T) {
	chdirRepoRoot(t)
	if err := runContractCheck(); err != nil {
		t.Fatalf("runContractCheck() error = %v", err)
	}
}

func TestContractCheckDetectsTamperedSnapshot(t *testing.T) {
	chdirRepoRoot(t)
	dir := t.TempDir()
	tampered := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(tampered, []byte("openapi: 3.0.0\npaths:\n  # tampered\n"), 0o600); err != nil {
		t.Fatalf("write tampered snapshot: %v", err)
	}
	t.Setenv("CONTRACT_SNAPSHOT_PATH", tampered)

	err := runContractCheck()
	if err == nil || !strings.Contains(err.Error(), "snapshot digest does not match provenance") {
		t.Fatalf("runContractCheck() error = %v, want digest mismatch marker", err)
	}
}

func TestContractGenerateIsDeterministic(t *testing.T) {
	chdirRepoRoot(t)
	if _, err := exec.LookPath("oapi-codegen"); err != nil {
		t.Skip("oapi-codegen not installed; run `make tools`")
	}
	if err := runContractGenerate(context.Background()); err != nil {
		t.Fatalf("runContractGenerate() error = %v", err)
	}
	first, err := os.ReadFile(contractGeneratedPath)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	if err := runContractGenerate(context.Background()); err != nil {
		t.Fatalf("runContractGenerate() second run error = %v", err)
	}
	second, err := os.ReadFile(contractGeneratedPath)
	if err != nil {
		t.Fatalf("read generated: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("contract generation is not deterministic")
	}
}

func TestContractCheckAllowlistIssues(t *testing.T) {
	chdirRepoRoot(t)

	cases := []struct {
		name        string
		contents    string
		wantMessage string
	}{
		{
			name:        "unknown operation",
			contents:    "operations:\n  - not.a.real.operation\n",
			wantMessage: "allowlisted operation not in the snapshot",
		},
		{
			name:        "duplicate entry",
			contents:    "operations:\n  - auth.refresh\n  - auth.refresh\n",
			wantMessage: "duplicate allowlist entry",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "allowlist.yaml")
			if err := os.WriteFile(path, []byte(tc.contents), 0o600); err != nil {
				t.Fatalf("write allowlist: %v", err)
			}
			t.Setenv("CONTRACT_ALLOWLIST_PATH", path)
			err := runContractCheck()
			if err == nil || !strings.Contains(err.Error(), tc.wantMessage) {
				t.Fatalf("runContractCheck() error = %v, want marker %q", err, tc.wantMessage)
			}
		})
	}
}

func runGitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitOutputInTest(t, dir, args...)
}

func gitOutputInTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := commandOutput(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return output
}
