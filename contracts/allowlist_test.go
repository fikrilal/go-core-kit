package contracts

import (
	"slices"
	"testing"
)

func TestParseAllowlist(t *testing.T) {
	contents := []byte("# header\noperations:\n  # group\n  - auth.refresh\n  - users.me.get\n\n  - admin.whoami.get # inline note\n")
	got := ParseAllowlist(contents)
	want := []string{"auth.refresh", "users.me.get", "admin.whoami.get"}
	if !slices.Equal(got, want) {
		t.Fatalf("ParseAllowlist() = %v, want %v", got, want)
	}
	if ids := ParseAllowlist([]byte("# nothing here\n")); len(ids) != 0 {
		t.Fatalf("ParseAllowlist(empty) = %v, want none", ids)
	}
}
