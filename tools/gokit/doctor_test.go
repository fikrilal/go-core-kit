package main

import "testing"

func TestParsePins(t *testing.T) {
	content := "GOLANGCI_LINT_VERSION ?= v2.14.0\nSQLC_VERSION ?= v1.31.1\nOAPI_CODEGEN_VERSION ?= v2.8.0\nVACUUM_VERSION ?= v0.33.0\nGOOSE_VERSION ?= v3.28.0\nGO ?= go\n"
	pins := parsePins(content)
	if pins["GOLANGCI_LINT_VERSION"] != "v2.14.0" || pins["SQLC_VERSION"] != "v1.31.1" || pins["OAPI_CODEGEN_VERSION"] != "v2.8.0" || pins["VACUUM_VERSION"] != "v0.33.0" || pins["GOOSE_VERSION"] != "v3.28.0" {
		t.Fatalf("parsePins() = %v", pins)
	}
	if _, ok := pins["GO"]; ok {
		t.Errorf("generic variables must not be captured: %v", pins)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.27.2", "1.27.2", 0},
		{"v1.31.1", "1.31.1", 0},
		{"1.27.3", "1.27.2", 1},
		{"1.27.2", "1.28.0", -1},
		{"1.27", "1.27.2", -1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestGoDirectivePattern(t *testing.T) {
	match := goDirectivePattern.FindStringSubmatch("module x\n\ngo 1.27.2\n")
	if match == nil || match[1] != "1.27.2" {
		t.Fatalf("go directive match = %v, want 1.27.2", match)
	}
}
