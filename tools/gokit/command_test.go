package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want command
	}{
		{name: "verify default", args: []string{"verify"}, want: verifyCommand{profile: "full"}},
		{name: "verify fast", args: []string{"verify", "--profile", "fast"}, want: verifyCommand{profile: "fast"}},
		{name: "doctor", args: []string{"doctor"}, want: doctorCommand{}},
		{name: "db generate", args: []string{"db", "generate"}, want: dbGenerateCommand{}},
		{name: "knowledge check", args: []string{"knowledge", "check"}, want: knowledgeCommand{}},
		{name: "contract check", args: []string{"contract", "check"}, want: contractCheckCommand{}},
		{name: "contract generate", args: []string{"contract", "generate"}, want: contractGenerateCommand{}},
		{name: "contract sync", args: []string{"contract", "sync", "--source", "openapi.yaml"}, want: contractSyncCommand{source: "openapi.yaml"}},
		{name: "contract sync equals", args: []string{"contract", "sync", "--source=openapi.yaml"}, want: contractSyncCommand{source: "openapi.yaml"}},
		{name: "gates honesty", args: []string{"gates", "honesty"}, want: gatesHonestyCommand{}},
		{name: "drift check", args: []string{"drift", "check"}, want: driftCheckCommand{}},
		{name: "help", args: []string{"help"}, want: helpCommand{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCommand(tc.args)
			if err != nil {
				t.Fatalf("parseCommand(%v) error = %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("parseCommand(%v) = %#v, want %#v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseCommandUsageErrors(t *testing.T) {
	cases := [][]string{
		{},
		{"nope"},
		{"verify", "--profile"},
		{"verify", "--profile", "turbo"},
		{"verify", "extra"},
		{"doctor", "now"},
		{"db"},
		{"db", "drop"},
		{"knowledge"},
		{"contract"},
		{"contract", "sync"},
		{"contract", "check", "now"},
		{"contract", "drop"},
		{"gates"},
		{"gates", "lies"},
		{"drift"},
		{"drift", "now"},
		{"help", "me"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := parseCommand(args)
			var usage *usageError
			if !errors.As(err, &usage) {
				t.Fatalf("parseCommand(%v) error = %v, want usageError", args, err)
			}
		})
	}
}
