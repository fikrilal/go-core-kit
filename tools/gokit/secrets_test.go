package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// secretsFixture builds a throwaway git repository with the given tracked files.
func secretsFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		//nolint:gosec // G204: the arguments are the fixed command lists below.
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	// Another test may have chdir'd into a directory that its own cleanup has
	// already removed, so the process never depends on the previous cwd.
	if err := os.Chdir(os.TempDir()); err != nil {
		t.Fatalf("chdir to temp: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(os.TempDir()) })
	return root
}

func TestSecretsScanFindsShapedText(t *testing.T) {
	secretsFixture(t, map[string]string{
		"config/keys.go": "package config\n\nconst key = " + "\"-----BEGIN RSA " + "\"PRIVATE KEY-----\"\n",
		"docs/aws.md":    "example: AKIA" + "IOSFODNN7EXAMPLE\n",
	})
	err := runSecretsScan(context.Background())
	if err == nil {
		t.Fatal("runSecretsScan() error = nil, want the private key and access key findings")
	}
}

func TestSecretsScanPassesCleanTrees(t *testing.T) {
	secretsFixture(t, map[string]string{
		"config/config.go": "package config\n\n// Secrets come from the environment.\nconst keyEnv = \"RESEND_API_KEY\"\n",
		"README.md":        "Documentation without any credential shapes.\n",
	})
	if err := runSecretsScan(context.Background()); err != nil {
		t.Fatalf("runSecretsScan() error = %v", err)
	}
}

func TestSecretsScanHonorsTheAllowlist(t *testing.T) {
	root := secretsFixture(t, map[string]string{
		"tools/gokit/secrets.go": "package main\n\n// -----BEGIN PRIVATE " + "\"KEY----- pattern definition\n",
	})
	if err := runSecretsScan(context.Background()); err != nil {
		t.Fatalf("runSecretsScan() error = %v, want the allowlist to skip %s", err, root)
	}
}
