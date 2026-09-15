package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/fikrilal/go-core-kit/contracts"
)

var (
	openapiVersionPattern = regexp.MustCompile(`(?m)^openapi:\s+3\.\d+\.\d+\s*$`)
	openapiPathsPattern   = regexp.MustCompile(`(?m)^paths:\s*$`)
	operationIDPattern    = regexp.MustCompile(`(?m)^\s+operationId:\s*(\S+)\s*$`)
)

const (
	snapshotDefaultPath   = "contracts/backend.openapi.yaml"
	provenanceDefaultPath = "contracts/provenance.json"
	allowlistDefaultPath  = "contracts/allowlist.yaml"

	contractGeneratedPath = "contracts/generated.go"
	oapiCodegenConfigPath = "oapi-codegen.yaml"
)

type provenance struct {
	SchemaVersion    int    `json:"schemaVersion"`
	SourceRepository string `json:"sourceRepository"`
	SourceCommit     string `json:"sourceCommit"`
	SourcePath       string `json:"sourcePath"`
	Sha256           string `json:"sha256"`
}

func contractPaths() (string, string) {
	return envOrDefault("CONTRACT_SNAPSHOT_PATH", snapshotDefaultPath),
		envOrDefault("CONTRACT_PROVENANCE_PATH", provenanceDefaultPath)
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func contractAllowlistPath() string {
	return envOrDefault("CONTRACT_ALLOWLIST_PATH", allowlistDefaultPath)
}

func runContractSync(ctx context.Context, sourcePath string) error {
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("contract sync: resolve source: %w", err)
	}
	contents, err := os.ReadFile(absSource) //nolint:gosec // source path is an explicit operator argument
	if err != nil {
		return fmt.Errorf("contract sync: read source: %w", err)
	}
	if err := validateSnapshot(contents); err != nil {
		return fmt.Errorf("contract sync: %w", err)
	}
	pinned, err := deriveProvenance(ctx, absSource, contents)
	if err != nil {
		return fmt.Errorf("contract sync: %w", err)
	}
	encoded, err := json.MarshalIndent(pinned, "", "  ")
	if err != nil {
		return fmt.Errorf("contract sync: encode provenance: %w", err)
	}

	snapshotPath, provenancePath := contractPaths()
	if err := writeFileAtomic(snapshotPath, contents); err != nil {
		return fmt.Errorf("contract sync: %w", err)
	}
	if err := writeFileAtomic(provenancePath, append(encoded, '\n')); err != nil {
		return fmt.Errorf("contract sync: %w", err)
	}
	fmt.Printf("contract: pinned %s from %s (%s)\n", pinned.SourcePath, shortCommit(pinned.SourceCommit), pinned.Sha256)
	return nil
}

func runContractCheck() error {
	snapshotPath, provenancePath := contractPaths()
	snapshot, err := os.ReadFile(snapshotPath) //nolint:gosec // pinned repo path, overridable for gate honesty
	if err != nil {
		return fmt.Errorf("contract check: read snapshot: %w", err)
	}
	rawProvenance, err := os.ReadFile(provenancePath) //nolint:gosec // pinned repo path, overridable for gate honesty
	if err != nil {
		return fmt.Errorf("contract check: read provenance: %w", err)
	}
	var pinned provenance
	if err := json.Unmarshal(rawProvenance, &pinned); err != nil {
		return fmt.Errorf("contract check: decode provenance: %w", err)
	}
	if err := validateSnapshot(snapshot); err != nil {
		return fmt.Errorf("contract check: %w", err)
	}
	digest := sha256Hex(snapshot)
	if digest != pinned.Sha256 {
		return fmt.Errorf(
			"contract: snapshot digest does not match provenance (want %s, got %s) — re-pin with `gokit contract sync`",
			pinned.Sha256, digest,
		)
	}
	declared, err := checkAllowlist(snapshot)
	if err != nil {
		return err
	}
	fmt.Printf("ok    snapshot digest matches provenance (%s from %s)\n", shortCommit(pinned.SourceCommit), pinned.SourceRepository)
	fmt.Printf("ok    allowlist: %d pending, %d not allowlisted, %d declared\n", len(declared.pending), len(declared.operations)-len(declared.pending), len(declared.operations))
	return nil
}

type allowlistState struct {
	operations []string
	pending    []string
}

func checkAllowlist(snapshot []byte) (allowlistState, error) {
	rawAllowlist, err := os.ReadFile(contractAllowlistPath())
	if err != nil {
		return allowlistState{}, fmt.Errorf("contract check: read allowlist: %w", err)
	}
	operations := snapshotOperationIDs(snapshot)
	pending := contracts.ParseAllowlist(rawAllowlist)

	seen := map[string]bool{}
	var issues []string
	for _, id := range pending {
		if seen[id] {
			issues = append(issues, "duplicate allowlist entry: "+id)
		}
		seen[id] = true
		if !slices.Contains(operations, id) {
			issues = append(issues, "allowlisted operation not in the snapshot: "+id)
		}
	}
	if len(issues) > 0 {
		return allowlistState{}, fmt.Errorf("contract: allowlist issues:\n- %s", strings.Join(issues, "\n- "))
	}
	return allowlistState{operations: operations, pending: pending}, nil
}

func snapshotOperationIDs(snapshot []byte) []string {
	var ids []string
	for _, match := range operationIDPattern.FindAllStringSubmatch(string(snapshot), -1) {
		ids = append(ids, strings.Trim(match[1], `"'`))
	}
	return ids
}

func runContractGenerate(ctx context.Context) error {
	snapshotPath, _ := contractPaths()
	cmd := exec.CommandContext(ctx, "oapi-codegen", "-config", oapiCodegenConfigPath, snapshotPath) //nolint:gosec // fixed tool and repo paths
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("contract generate: oapi-codegen failed (install with `make tools`): %w", err)
	}
	fmt.Printf("contract: regenerated %s\n", contractGeneratedPath)
	return nil
}

func deriveProvenance(ctx context.Context, absSource string, contents []byte) (provenance, error) {
	dir := filepath.Dir(absSource)
	gitRoot, err := commandOutput(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return provenance{}, fmt.Errorf("source is not in a git repository: %w", err)
	}
	root, err := filepath.EvalSymlinks(gitRoot)
	if err != nil {
		return provenance{}, fmt.Errorf("resolve repository root: %w", err)
	}
	source, err := filepath.EvalSymlinks(absSource)
	if err != nil {
		return provenance{}, fmt.Errorf("resolve source path: %w", err)
	}
	relative, err := filepath.Rel(root, source)
	if err != nil {
		return provenance{}, fmt.Errorf("relativize source path: %w", err)
	}
	relative = filepath.ToSlash(relative)

	if _, err := commandOutput(ctx, "git", "-C", root, "ls-files", "--error-unmatch", "--", relative); err != nil {
		return provenance{}, fmt.Errorf("source file is not tracked: %s", relative)
	}
	status, err := commandOutput(ctx, "git", "-C", root, "status", "--porcelain", "--", relative)
	if err != nil {
		return provenance{}, fmt.Errorf("inspect source status: %w", err)
	}
	if status != "" {
		return provenance{}, fmt.Errorf("source file has uncommitted changes: %s", relative)
	}

	repository, _ := commandOutput(ctx, "git", "-C", root, "config", "--get", "remote.origin.url")
	if repository == "" {
		repository = filepath.Base(root)
	}
	commit, err := commandOutput(ctx, "git", "-C", root, "rev-parse", "HEAD")
	if err != nil {
		return provenance{}, fmt.Errorf("read source commit: %w", err)
	}

	return provenance{
		SchemaVersion:    1,
		SourceRepository: repository,
		SourceCommit:     commit,
		SourcePath:       relative,
		Sha256:           sha256Hex(contents),
	}, nil
}

func validateSnapshot(contents []byte) error {
	if !openapiVersionPattern.Match(contents) {
		return fmt.Errorf("source is not an OpenAPI 3.x document")
	}
	if !openapiPathsPattern.Match(contents) {
		return fmt.Errorf("source does not declare paths")
	}
	return nil
}

func sha256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func writeFileAtomic(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // repository artifact directory
		return fmt.Errorf("write %s: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
