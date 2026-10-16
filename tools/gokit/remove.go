package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// removeFeatureCommand removes a feature and unwires it from the kit.
type removeFeatureCommand struct {
	name string
}

func (c removeFeatureCommand) execute(context.Context) error {
	return removeFeature(c.name)
}

// routeIDPattern finds the contract operations a feature implements.
var routeIDPattern = regexp.MustCompile(`ID: "([A-Za-z0-9_.]+)"`)

// removeFeature unwires the feature from the composition root, returns its
// operations to the contract allowlist (the coverage test compares the two),
// and deletes the files the scaffold wrote for it. Text edits happen before any
// deletion, so a refused removal leaves every file in place.
func removeFeature(name string) error {
	if !featureNamePattern.MatchString(name) {
		return usagef("remove feature: name must be lower-case letters and digits (got %q)", name)
	}
	dir := filepath.Join(featureRootDir, name)
	if _, err := os.Stat(dir); err != nil {
		return usagef("remove feature: %s does not exist", dir)
	}
	operations, err := featureOperations(dir)
	if err != nil {
		return err
	}
	if err := unwireFeature(name); err != nil {
		return err
	}
	if err := restoreAllowlist(operations); err != nil {
		return err
	}
	removed, err := deleteFeatureFiles(name, dir)
	if err != nil {
		return err
	}
	fmt.Printf("remove feature: %s unwired, %d file(s) removed\n", name, len(removed))
	for _, path := range removed {
		fmt.Println("  removed " + path)
	}
	if len(operations) > 0 {
		fmt.Println("allowlist restored: " + strings.Join(operations, ", "))
	}
	fmt.Println("next: `make generate` for the removed query, then `make verify`")
	return nil
}

// featureOperations lists the contract operation ids the feature routes.
func featureOperations(dir string) ([]string, error) {
	seen := map[string]bool{}
	//nolint:gosec // G122: the walk is rooted at the validated feature directory.
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".go" {
			return err
		}
		// #nosec G304 -- the walk stays inside the feature directory.
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for _, match := range routeIDPattern.FindAllStringSubmatch(string(data), -1) {
			seen[match[1]] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("remove feature: scan %s: %w", dir, err)
	}
	operations := make([]string, 0, len(seen))
	for id := range seen {
		operations = append(operations, id)
	}
	sort.Strings(operations)
	return operations, nil
}

// unwireFeature drops the feature's import and Register call from the
// composition root, leaving the markers for the next scaffold.
func unwireFeature(name string) error {
	// #nosec G304 -- the path is a fixed repository path.
	data, err := os.ReadFile(serverPath)
	if err != nil {
		return fmt.Errorf("remove feature: read %s: %w", serverPath, err)
	}
	content := string(data)
	importLine := "\t\"github.com/fikrilal/go-core-kit/internal/features/" + name + "\"\n"
	registrationPrefix := "\t" + name + ".Register(router, "
	if !strings.Contains(content, registrationPrefix) {
		return usagef("remove feature: %s does not register %s", serverPath, name)
	}
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == strings.TrimSuffix(importLine, "\n") || strings.HasPrefix(line, registrationPrefix) {
			continue
		}
		kept = append(kept, line)
	}
	updated, err := formatGo(serverPath, strings.Join(kept, "\n"))
	if err != nil {
		return err
	}
	// #nosec G703 -- the path is a fixed repository path.
	if err := os.WriteFile(serverPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("remove feature: write %s: %w", serverPath, err)
	}
	return nil
}

// restoreAllowlist lists the feature's operations as unimplemented again.
func restoreAllowlist(operations []string) error {
	if len(operations) == 0 {
		return nil
	}
	// #nosec G304 -- the path is a fixed repository path.
	data, err := os.ReadFile(allowlistDefaultPath)
	if err != nil {
		return fmt.Errorf("remove feature: read %s: %w", allowlistDefaultPath, err)
	}
	content := string(data)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	for _, id := range operations {
		if strings.Contains(content, "  - "+id+"\n") {
			continue
		}
		content += "  - " + id + "\n"
	}
	// #nosec G703 -- the path is a fixed repository path.
	if err := os.WriteFile(allowlistDefaultPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("remove feature: write %s: %w", allowlistDefaultPath, err)
	}
	return nil
}

// deleteFeatureFiles removes the feature and the per-feature files the scaffold
// wrote outside it.
func deleteFeatureFiles(name, dir string) ([]string, error) {
	removed := []string{}
	contractTests, err := filepath.Glob(filepath.Join("cmd", "api", "contract_"+name+"_*_test.go"))
	if err != nil {
		return nil, fmt.Errorf("remove feature: glob contract tests: %w", err)
	}
	paths := make([]string, 0, len(contractTests)+1)
	paths = append(paths, contractTests...)
	paths = append(paths, filepath.Join("db", "queries", "common", name+".sql"))
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		// #nosec G703 -- the path is derived from the validated feature name.
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove feature: remove %s: %w", path, err)
		}
		removed = append(removed, path)
	}
	// #nosec G304 G703 -- the directory is derived from the validated feature name.
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("remove feature: remove %s: %w", dir, err)
	}
	removed = append(removed, dir+"/")
	return removed, nil
}
