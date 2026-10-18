package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tagged path counts for the pg lane audit. Shrinking these is the point of the
// pg work; growing them needs a deliberate edit here and in the audit note.
const (
	taggedForPgLift   = 3
	taggedForPgStay   = 6
	taggedForPgSwitch = 0
)

func TestPortableSubsetCountsMatchTheTree(t *testing.T) {
	counts := map[string]int{"lift": 0, "stay": 0, "switch": 0}
	for _, root := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join("..", "..", root), func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			//nolint:gosec // G304: the walk stays inside the repository tree.
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			if !strings.Contains(string(data), "//go:build !pg") {
				return nil
			}
			if kind := classifyTaggedPath(filepath.ToSlash(strings.TrimPrefix(path, "../../"))); kind != "" {
				counts[kind]++
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if counts["lift"] != taggedForPgLift || counts["stay"] != taggedForPgStay || counts["switch"] != taggedForPgSwitch {
		t.Fatalf("tagged paths = %v, want lift=%d stay=%d switch=%d — update the audit note when this changes",
			counts, taggedForPgLift, taggedForPgStay, taggedForPgSwitch)
	}
}

// classifyTaggedPath names the bucket a hand-written tagged path belongs to, and
// returns "" for paths the audit does not count: generated code owns its own tags.
func classifyTaggedPath(relative string) string {
	switch {
	case strings.HasPrefix(relative, "internal/db/gen/"):
		return ""
	case relative == "cmd/api/main.go":
		return "switch"
	case strings.Contains(relative, "sqlite"):
		return "stay"
	default:
		return "lift"
	}
}
