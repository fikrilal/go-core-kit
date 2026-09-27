// Package contracts carries the pinned wire contract: the snapshot, its provenance, generated types, and readers for the contract artifacts.
package contracts

import "strings"

// ParseAllowlist scans lines. Any "- " item is an id. "#" starts a comment. There are no YAML types.
func ParseAllowlist(contents []byte) []string {
	var ids []string
	for line := range strings.Lines(string(contents)) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if index := strings.Index(id, "#"); index >= 0 {
			id = strings.TrimSpace(id[:index])
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}
