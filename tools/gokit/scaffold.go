package main

import (
	"context"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	featureRegistrationMarker = "// gokit:feature-registrations"
	routeRegistrationMarker   = "// gokit:route-registrations"
	featureRootDir            = "internal/features"
	serverPath                = "cmd/api/server.go"
)

// scaffoldFeatureCommand creates a feature skeleton and wires it into the
// composition root.
type scaffoldFeatureCommand struct {
	name string
}

func (c scaffoldFeatureCommand) execute(context.Context) error {
	return scaffoldFeature(c.name)
}

// scaffoldEndpointCommand implements one snapshot operation in a feature.
type scaffoldEndpointCommand struct {
	feature   string
	operation string
}

func (c scaffoldEndpointCommand) execute(context.Context) error {
	return scaffoldEndpoint(c.feature, c.operation)
}

// featureNamePattern keeps the scaffolded package a valid identifier.
var featureNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// scaffoldFeature writes the feature skeleton and registers it in the API.
func scaffoldFeature(name string) error {
	if !featureNamePattern.MatchString(name) {
		return usagef("scaffold feature: name must be lower-case letters and digits (got %q)", name)
	}
	dir := filepath.Join(featureRootDir, name)
	if _, err := os.Stat(dir); err == nil {
		return usagef("scaffold feature: %s already exists", dir)
	}

	generated := map[string]string{
		filepath.Join(dir, "domain", "domain.go"):    featureDomainTemplate(name),
		filepath.Join(dir, "ports", "ports.go"):      featurePortsTemplate(name),
		filepath.Join(dir, "infra", "repository.go"): featureRepositoryTemplate(name),
		filepath.Join(dir, "app", "service.go"):      featureServiceTemplate(name),
		filepath.Join(dir, name+".go"):               featureRootTemplate(name),
		filepath.Join(dir, name+"_test.go"):          featureRootTestTemplate(name),
	}
	writes := &stagedWrites{}
	if err := writes.commit(generated); err != nil {
		return err
	}
	if err := insertFeatureRegistration(name); err != nil {
		writes.rollback()
		return err
	}
	fmt.Printf("scaffold feature: created %s and wired it in %s\n", dir, serverPath)
	fmt.Println("next: `make verify` to confirm, then `gokit scaffold endpoint --feature " + name + " --operation <id>`")
	return nil
}

// stagedWrites writes files only when they are all writable, and can undo
// itself, so a failed step never leaves a half-written feature.
type stagedWrites struct {
	paths []string
}

func (w *stagedWrites) commit(files map[string]string) error {
	for path, content := range files {
		if _, err := os.Stat(path); err == nil {
			return usagef("scaffold: %s already exists", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("scaffold: create %s: %w", filepath.Dir(path), err)
		}
		formatted, err := formatGo(path, content)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(formatted), 0o600); err != nil {
			return fmt.Errorf("scaffold: write %s: %w", path, err)
		}
		w.paths = append(w.paths, path)
	}
	return nil
}

// formatGo runs gofmt over generated Go files, so template spacing can never
// leak into the tree.
func formatGo(path, content string) (string, error) {
	if filepath.Ext(path) != ".go" {
		return content, nil
	}
	formatted, err := format.Source([]byte(content))
	if err != nil {
		return "", fmt.Errorf("scaffold: format %s: %w", path, err)
	}
	return string(formatted), nil
}

// rollback removes what commit wrote.
func (w *stagedWrites) rollback() {
	for _, path := range w.paths {
		_ = os.Remove(path)
	}
}

// insertFeatureRegistration adds the Register call between the composition
// root's markers, keeping 6.2's removal textual.
func insertFeatureRegistration(name string) error {
	// #nosec G304 -- the path is a fixed repository path.
	data, err := os.ReadFile(serverPath)
	if err != nil {
		return fmt.Errorf("scaffold: read %s: %w", serverPath, err)
	}
	content := string(data)
	if !strings.Contains(content, featureRegistrationMarker) {
		return usagef("scaffold: %s has no %q marker", serverPath, featureRegistrationMarker)
	}
	if strings.Contains(content, `"github.com/fikrilal/go-core-kit/internal/features/`+name+`"`) {
		return usagef("scaffold: %s already wires the %s feature", serverPath, name)
	}
	line := "\t" + name + ".Register(router, " + name + ".Deps{Database: database, Clock: clock.System{}, Guard: guard})\n"
	markerLine := featureRegistrationMarker + " — `gokit scaffold feature` inserts here.\n"
	updated := strings.Replace(content, markerLine, markerLine+line, 1)
	updated = strings.Replace(updated, "\t\"github.com/fikrilal/go-core-kit/internal/features/auth\"",
		"\t\"github.com/fikrilal/go-core-kit/internal/features/auth\"\n\t\"github.com/fikrilal/go-core-kit/internal/features/"+name+"\"", 1)
	formatted, err := formatGo(serverPath, updated)
	if err != nil {
		return err
	}
	// #nosec G703 -- the path is a fixed repository path.
	if err := os.WriteFile(serverPath, []byte(formatted), 0o600); err != nil {
		return fmt.Errorf("scaffold: write %s: %w", serverPath, err)
	}
	return nil
}

// scaffoldEndpoint implements one allowlisted operation: it resolves the
// operation in the pinned snapshot, writes the use case, repository method, and
// route, adds a contract test, and drops the operation from the allowlist.
func scaffoldEndpoint(feature, operation string) error {
	if !featureNamePattern.MatchString(feature) {
		return usagef("scaffold endpoint: feature name must be lower-case letters and digits (got %q)", feature)
	}
	dir := filepath.Join(featureRootDir, feature)
	if _, err := os.Stat(dir); err != nil {
		return usagef("scaffold endpoint: feature %s does not exist — run `gokit scaffold feature --name %s` first", feature, feature)
	}
	spec, err := resolveOperation(operation)
	if err != nil {
		return err
	}
	// #nosec G304 -- the path is a fixed repository path.
	allowlist, err := os.ReadFile(allowlistDefaultPath)
	if err != nil {
		return fmt.Errorf("scaffold: read %s: %w", allowlistDefaultPath, err)
	}
	entry := "  - " + operation + "\n"
	if !strings.Contains(string(allowlist), entry) {
		return usagef("scaffold endpoint: %s is not allowlisted (already implemented, or not declared by the pinned snapshot)", operation)
	}

	slug := spec.slug()
	generated := map[string]string{
		filepath.Join(dir, "app", slug+".go"):                                endpointServiceTemplate(feature, spec),
		filepath.Join(dir, "infra", slug+".go"):                              endpointRepositoryTemplate(feature, spec),
		filepath.Join(dir, "app", slug+"_test.go"):                           endpointServiceTestTemplate(feature, spec),
		filepath.Join("cmd", "api", "contract_"+feature+"_"+slug+"_test.go"): endpointContractTestTemplate(feature, spec),
		filepath.Join("db", "queries", "common", feature+".sql"):             endpointQueryTemplate(spec),
	}
	// A second endpoint appends to the feature's query file rather than
	// dropping its query on the floor.
	queryPath := filepath.Join("db", "queries", "common", feature+".sql")
	appendQuery := ""
	//nolint:gosec // G304: the path derives from the validated feature name.
	if existing, err := os.ReadFile(queryPath); err == nil {
		appendQuery = string(existing) + "\n" + endpointQueryTemplate(spec)
		delete(generated, queryPath)
	}
	writes := &stagedWrites{}
	if err := writes.commit(generated); err != nil {
		return err
	}
	if appendQuery != "" {
		// #nosec G703 -- the path derives from the validated feature name.
		if err := os.WriteFile(queryPath, []byte(appendQuery), 0o600); err != nil {
			writes.rollback()
			return fmt.Errorf("scaffold: append %s: %w", queryPath, err)
		}
	}
	if err := registerRoute(dir, feature, spec); err != nil {
		writes.rollback()
		return err
	}
	updated := strings.Replace(string(allowlist), entry, "", 1)
	// #nosec G703 -- the path is a fixed repository path.
	if err := os.WriteFile(allowlistDefaultPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("scaffold: write %s: %w", allowlistDefaultPath, err)
	}
	fmt.Printf("scaffold endpoint: %s %s implemented in %s/%s\n", spec.method, spec.path, featureRootDir, feature)
	fmt.Println("next: `make generate` for the new query, then `make verify`")
	return nil
}

// operationSpec is what the snapshot declares about one operation.
type operationSpec struct {
	id       string
	method   string
	path     string
	function string
}

// resolveOperation finds the operation in the pinned snapshot and derives its
// method and path from the surrounding YAML.
func resolveOperation(operation string) (operationSpec, error) {
	// #nosec G304 -- the path is a fixed repository path.
	data, err := os.ReadFile(snapshotDefaultPath)
	if err != nil {
		return operationSpec{}, fmt.Errorf("scaffold: read %s: %w", snapshotDefaultPath, err)
	}
	lines := strings.Split(string(data), "\n")
	line := -1
	for i, text := range lines {
		if strings.TrimSpace(text) == "operationId: "+operation {
			line = i
			break
		}
	}
	if line < 0 {
		return operationSpec{}, usagef("scaffold endpoint: operation %q is not in the pinned snapshot", operation)
	}
	method := ""
	path := ""
	for i := line; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if method == "" && methodPattern.MatchString(trimmed) {
			method = strings.TrimSuffix(trimmed, ":")
		}
		if path == "" && strings.HasPrefix(trimmed, "/") && strings.HasSuffix(trimmed, ":") {
			path = strings.TrimSuffix(trimmed, ":")
		}
		if method != "" && path != "" {
			break
		}
	}
	if method == "" || path == "" {
		return operationSpec{}, usagef("scaffold endpoint: could not resolve the method and path for %q", operation)
	}
	return operationSpec{id: operation, method: strings.ToUpper(method), path: path, function: operationFunction(operation)}, nil
}

var methodPattern = regexp.MustCompile(`^(get|post|put|patch|delete):$`)

// useCase is the exported name of the use case and its types.
func (s operationSpec) useCase() string {
	name := s.function
	name = strings.TrimPrefix(name, "handle")
	return name
}

// slug is the file name for this operation.
func (s operationSpec) slug() string { return operationSlug(s.id) }

// operationFunction is the handler name for an operation id.
func operationFunction(operation string) string {
	parts := strings.Split(operation, ".")
	// The domain prefix is the feature, so the resource and the verb are what
	// distinguish one operation from another.
	nameParts := parts[1:]
	if len(nameParts) == 0 {
		nameParts = parts
	}
	var words []string
	for _, part := range nameParts {
		words = append(words, splitWords(part)...)
	}
	if len(words) == 0 {
		return "handle"
	}
	words[0] = titleFirst(words[0])
	return "handle" + strings.Join(words, "")
}

// operationSlug is the file name for an operation id.
func operationSlug(operation string) string {
	parts := strings.Split(operation, ".")
	nameParts := parts[1:]
	if len(nameParts) == 0 {
		nameParts = parts
	}
	words := []string{}
	for _, part := range nameParts {
		words = append(words, splitWords(part)...)
	}
	return strings.ToLower(strings.Join(words, "-"))
}

// titleFirst upper-cases the first letter of a word.
func titleFirst(word string) string {
	if word == "" {
		return word
	}
	return strings.ToUpper(word[:1]) + word[1:]
}

func splitWords(value string) []string {
	var words []string
	current := ""
	for _, r := range value {
		switch {
		case r == '_' || r == '-':
			if current != "" {
				words = append(words, current)
				current = ""
			}
		case r >= 'A' && r <= 'Z':
			if current != "" {
				words = append(words, current)
			}
			current = string(r)
		default:
			current += string(r)
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}
