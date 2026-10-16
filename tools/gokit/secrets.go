package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// secretPatterns are the shapes that are never legitimate in tracked files. The
// scan reports the file and line, never the match, so a finding does not copy the
// secret into CI logs.
var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{name: "private key", pattern: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{name: "aws access key id", pattern: regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{name: "resend api key", pattern: regexp.MustCompile(`\bre_[A-Za-z0-9]{20,}\b`)},
	{name: "google service account", pattern: regexp.MustCompile(`"type"\s*:\s*"service_account"`)},
	{name: "pem certificate body", pattern: regexp.MustCompile(`-----BEGIN CERTIFICATE-----`)},
}

// secretsAllowlist names tracked files that legitimately contain a pattern — a
// fixture, a documented example, or the pattern's own definition. Each entry
// carries its reason; anything else is a finding.
var secretsAllowlist = map[string]string{
	"tools/gokit/secrets.go":                  "this file defines the patterns it searches for",
	"internal/platform/storage/sigv4_test.go": "the AWS-published SigV4 test vectors, not credentials",
}

// scannedFileLimit skips large tracked files: a secret in a binary blob is not
// what this gate is for, and the read would dominate the run.
const scannedFileLimit = 1 << 20

func runSecretsScan(ctx context.Context) error {
	output, err := commandOutput(ctx, "git", "ls-files")
	if err != nil {
		return fmt.Errorf("secrets: list tracked files: %w", err)
	}
	findings := 0
	for _, path := range strings.Split(strings.TrimSpace(output), "\n") {
		if path == "" {
			continue
		}
		if _, allowed := secretsAllowlist[path]; allowed {
			continue
		}
		for _, finding := range scanFileForSecrets(path) {
			findings++
			fmt.Printf("secrets: %s:%d looks like a %s\n", path, finding.line, finding.pattern)
		}
	}
	if findings > 0 {
		return fmt.Errorf("secrets: %d tracked file(s) contain secret-shaped text", findings)
	}
	fmt.Println("secrets: no secret-shaped text in tracked files")
	return nil
}

// secretFinding is one line that matched a pattern.
type secretFinding struct {
	line    int
	pattern string
}

// scanFileForSecrets reports the lines of one file that match a secret pattern.
// Unreadable, oversized, and binary entries are not findings.
func scanFileForSecrets(path string) []secretFinding {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > scannedFileLimit {
		return nil
	}
	//nolint:gosec // G304: the path comes from git's own tracked-file list.
	data, err := os.ReadFile(path)
	if err != nil || isBinary(data) {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	var hits []secretFinding
	for i, line := range lines {
		for _, pattern := range secretPatterns {
			if pattern.pattern.MatchString(line) {
				hits = append(hits, secretFinding{line: i + 1, pattern: pattern.name})
				break
			}
		}
	}
	return hits
}

// isBinary reports whether the content looks like a binary file.
func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}
