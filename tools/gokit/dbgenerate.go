package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const (
	genDir     = "internal/db/gen"
	scratchDir = ".gen"
)

type engine struct {
	name   string
	tag    string
	dir    string
	prefix string
}

var engines = []engine{
	{name: "sqlite", tag: "!pg", dir: filepath.Join(scratchDir, "sqlite"), prefix: "sqlite_"},
	{name: "pg", tag: "pg", dir: filepath.Join(scratchDir, "pg"), prefix: "pg_"},
}

func generateDB() error {
	if _, err := os.Stat("go.mod"); err != nil {
		return errors.New("run from the repository root (go.mod not found)")
	}
	if err := os.RemoveAll(scratchDir); err != nil {
		return fmt.Errorf("clean scratch: %w", err)
	}
	// The scratch directory is sqlc's workspace, never a tree resident: it goes
	// away whether generation succeeds or fails.
	defer func() {
		if err := os.RemoveAll(scratchDir); err != nil {
			fmt.Fprintf(os.Stderr, "gokit: clean scratch: %v\n", err)
		}
	}()
	if err := runSqlc(); err != nil {
		return err
	}
	for _, eng := range engines {
		if err := packageEngine(eng); err != nil {
			return fmt.Errorf("package %s output: %w", eng.name, err)
		}
	}
	fmt.Println("gokit: regenerated " + genDir)
	return nil
}

func runSqlc() error {
	cmd := exec.CommandContext(context.Background(), "sqlc", "generate", "-f", "sqlc.yaml")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlc generate failed (install sqlc via make tools): %w", err)
	}
	return nil
}

func packageEngine(eng engine) error {
	names, err := engineFiles(eng.dir)
	if err != nil {
		return err
	}
	if err := removeStale(genDir, eng.prefix); err != nil {
		return err
	}
	for _, name := range names {
		source := filepath.Join(eng.dir, name)
		content, err := os.ReadFile(source) //nolint:gosec // path enumerated from our own scratch output
		if err != nil {
			return fmt.Errorf("read %s: %w", source, err)
		}
		target := filepath.Join(genDir, eng.prefix+name)
		tagged := addBuildTag(content, eng.tag)
		if err := os.WriteFile(target, tagged, 0o644); err != nil { //nolint:gosec // generated source files are world-readable by convention
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	return nil
}

func engineFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	return names, nil
}

func removeStale(dir, prefix string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".go") {
			continue
		}
		target := filepath.Join(dir, name)
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("remove %s: %w", target, err)
		}
	}
	return nil
}

func addBuildTag(content []byte, tag string) []byte {
	header := "//go:build " + tag + "\n\n"
	tagged := make([]byte, 0, len(header)+len(content))
	tagged = append(tagged, header...)
	tagged = append(tagged, content...)
	return tagged
}
