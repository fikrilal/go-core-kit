package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/fikrilal/go-core-kit/internal/platform/config"
)

var (
	envLinePattern      = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=`)
	fencePattern        = regexp.MustCompile("(?s)```(?:text)?\n(.*?)```")
	mapEntryPattern     = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_./-]*/)`)
	markdownLinkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	backtickPattern     = regexp.MustCompile("`([A-Za-z0-9_][A-Za-z0-9_./-]*)`")
	adrFilePattern      = regexp.MustCompile(`^\d{4}-.*\.md$`)
)

// plannedPaths are documented in the AGENTS.md map before their phase lands; shrink as phases ship.
var plannedPaths = []string{"cmd/worker/", "internal/shared/"}

var indexFilePrefixes = []string{"docs/", "_WIP/", "cmd/", "internal/", "db/", "tools/"}

type knowledgeCheck struct {
	name string
	run  func() ([]string, error)
}

func runKnowledgeCheck() error {
	checks := []knowledgeCheck{
		{name: "env.example <=> config", run: checkEnvExample},
		{name: "project map (AGENTS.md)", run: checkProjectMap},
		{name: "markdown links", run: checkMarkdownLinks},
		{name: "ADR index", run: checkADRIndex},
	}
	issues := 0
	for _, check := range checks {
		found, err := check.run()
		if err != nil {
			return fmt.Errorf("knowledge: %s: %w", check.name, err)
		}
		if len(found) == 0 {
			fmt.Printf("ok    %s\n", check.name)
			continue
		}
		for _, issue := range found {
			fmt.Printf("issue %s: %s\n", check.name, issue)
		}
		issues += len(found)
	}
	if issues > 0 {
		return fmt.Errorf("knowledge: %d issue(s)", issues)
	}
	return nil
}

func checkEnvExample() ([]string, error) {
	data, err := os.ReadFile("env.example")
	if err != nil {
		return nil, fmt.Errorf("read env.example: %w", err)
	}
	documented := parseEnvKeys(string(data))
	expected := configEnvKeys()
	var issues []string
	for _, key := range expected {
		if !slices.Contains(documented, key) {
			issues = append(issues, "missing in env.example: "+key)
		}
	}
	for _, key := range documented {
		if !slices.Contains(expected, key) {
			issues = append(issues, "not in the config schema: "+key)
		}
	}
	return issues, nil
}

func parseEnvKeys(content string) []string {
	var keys []string
	for _, line := range strings.Split(content, "\n") {
		if match := envLinePattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			keys = append(keys, match[1])
		}
	}
	slices.Sort(keys)
	return keys
}

func configEnvKeys() []string {
	var keys []string
	typ := reflect.TypeOf(config.Config{})
	for i := range typ.NumField() {
		name := strings.Split(typ.Field(i).Tag.Get("env"), ",")[0]
		if name != "" {
			keys = append(keys, name)
		}
	}
	slices.Sort(keys)
	return keys
}

func checkProjectMap() ([]string, error) {
	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		return nil, fmt.Errorf("read AGENTS.md: %w", err)
	}
	entries, err := parseProjectMap(string(data))
	if err != nil {
		return nil, err
	}
	var issues []string
	for _, entry := range entries {
		if slices.Contains(plannedPaths, entry) {
			continue
		}
		if _, err := os.Stat(entry); err != nil {
			issues = append(issues, "listed in AGENTS.md but missing on disk: "+entry)
		}
	}
	for _, top := range []string{"cmd/", "internal/", "db/", "tools/", "docs/"} {
		listed := slices.ContainsFunc(entries, func(entry string) bool {
			return strings.HasPrefix(entry, top)
		})
		if !listed {
			issues = append(issues, "top-level directory not in the AGENTS.md map: "+top)
		}
	}
	return issues, nil
}

func parseProjectMap(markdown string) ([]string, error) {
	for _, block := range fencePattern.FindAllStringSubmatch(markdown, -1) {
		if !strings.Contains(block[1], "cmd/api/") {
			continue
		}
		var entries []string
		for _, line := range strings.Split(block[1], "\n") {
			if match := mapEntryPattern.FindStringSubmatch(line); match != nil {
				entries = append(entries, match[1])
			}
		}
		return entries, nil
	}
	return nil, fmt.Errorf("architecture map block not found in AGENTS.md")
}

func checkMarkdownLinks() ([]string, error) {
	files := []string{"AGENTS.md", "README.md"}
	err := filepath.WalkDir("docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk docs: %w", err)
	}
	slices.Sort(files)

	var issues []string
	for _, file := range files {
		found, err := linkIssues(file)
		if err != nil {
			return nil, err
		}
		issues = append(issues, found...)
	}
	issues = append(issues, backtickPathIssues()...)
	return issues, nil
}

func linkIssues(file string) ([]string, error) {
	data, err := os.ReadFile(file) //nolint:gosec // repo-relative documentation path; read-only check
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var issues []string
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(string(data), -1) {
		target := match[1]
		if target == "" || strings.HasPrefix(target, "#") || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		target = strings.SplitN(target, "#", 2)[0]
		if target == "" {
			continue
		}
		resolved := filepath.Join(filepath.Dir(file), target)
		if strings.HasPrefix(target, "/") {
			resolved = strings.TrimPrefix(target, "/")
		}
		if _, err := os.Stat(resolved); err != nil { //nolint:gosec // repo-relative documentation path; read-only check
			issues = append(issues, fmt.Sprintf("%s: missing link target %q", file, target))
		}
	}
	return issues, nil
}

func backtickPathIssues() []string {
	var issues []string
	for _, file := range []string{"docs/README.md", "docs/adr/README.md"} {
		data, err := os.ReadFile(file) //nolint:gosec // repo-relative index file; read-only check
		if err != nil {
			issues = append(issues, fmt.Sprintf("%s: %v", file, err))
			continue
		}
		for _, match := range backtickPattern.FindAllStringSubmatch(string(data), -1) {
			target := match[1]
			if slices.ContainsFunc(indexFilePrefixes, func(prefix string) bool {
				return strings.HasPrefix(target, prefix)
			}) {
				if _, err := os.Stat(target); err != nil { //nolint:gosec // repo-relative documentation path; read-only check
					issues = append(issues, fmt.Sprintf("%s: missing referenced path %q", file, target))
				}
			}
		}
	}
	return issues
}

func checkADRIndex() ([]string, error) {
	entries, err := os.ReadDir("docs/adr")
	if err != nil {
		return nil, fmt.Errorf("read docs/adr: %w", err)
	}
	index, err := os.ReadFile("docs/adr/README.md")
	if err != nil {
		return nil, fmt.Errorf("read docs/adr/README.md: %w", err)
	}
	indexText := string(index)

	var issues []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !adrFilePattern.MatchString(name) {
			continue
		}
		if !strings.Contains(indexText, name) {
			issues = append(issues, "ADR not listed in docs/adr/README.md: "+name)
		}
	}
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(indexText, -1) {
		target := match[1]
		if !strings.HasPrefix(target, "./") {
			continue
		}
		resolved := filepath.Join("docs/adr", strings.TrimPrefix(target, "./"))
		if _, err := os.Stat(resolved); err != nil { //nolint:gosec // repo-relative documentation path; read-only check
			issues = append(issues, fmt.Sprintf("docs/adr/README.md: missing link target %q", target))
		}
	}
	return issues, nil
}
