package main

import (
	"os"
	"strings"
	"testing"
)

func TestExpectGateFailure(t *testing.T) {
	t.Run("succeeding gate is ineffective", func(t *testing.T) {
		err := expectGateFailure(t.Context(), "marker", os.Environ(), "true")
		if err == nil || !strings.Contains(err.Error(), "unexpectedly succeeded") {
			t.Fatalf("expectGateFailure() error = %v, want ineffective-gate error", err)
		}
	})
	t.Run("missing marker is reported", func(t *testing.T) {
		err := expectGateFailure(t.Context(), "never-printed", os.Environ(), "sh", "-c", "echo nope; exit 1")
		if err == nil || !strings.Contains(err.Error(), "without the expected marker") {
			t.Fatalf("expectGateFailure() error = %v, want marker error", err)
		}
	})
	t.Run("expected marker passes", func(t *testing.T) {
		err := expectGateFailure(t.Context(), "boom", os.Environ(), "sh", "-c", "echo boom; exit 1")
		if err != nil {
			t.Fatalf("expectGateFailure() error = %v, want nil", err)
		}
	})
}
