package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

const usageText = `gokit commands:
  verify --profile fast|full|ci   run a verification profile (default: full)
  doctor                          inspect local prerequisites
  db generate                     regenerate sqlc output into internal/db/gen
  knowledge check                 env.example, project map, links, ADR index
  help                            show this message`

type command interface {
	execute(ctx context.Context) error
}

type usageError struct {
	message string
}

func (e *usageError) Error() string { return e.message }

func usagef(format string, args ...any) error {
	return &usageError{message: fmt.Sprintf(format, args...)}
}

type verifyCommand struct {
	profile string
}

func (c verifyCommand) execute(ctx context.Context) error {
	return runProfile(ctx, c.profile)
}

type doctorCommand struct{}

func (doctorCommand) execute(ctx context.Context) error {
	return runDoctor(ctx)
}

type dbGenerateCommand struct{}

func (dbGenerateCommand) execute(context.Context) error {
	return generateDB()
}

type knowledgeCommand struct{}

func (knowledgeCommand) execute(context.Context) error {
	return runKnowledgeCheck()
}

type helpCommand struct{}

func (helpCommand) execute(context.Context) error {
	fmt.Println(usageText)
	return nil
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return nil, usagef("missing command")
	}
	switch args[0] {
	case "verify":
		return parseVerify(args[1:])
	case "doctor":
		if len(args) != 1 {
			return nil, usagef("doctor takes no arguments")
		}
		return doctorCommand{}, nil
	case "db":
		if len(args) != 2 || args[1] != "generate" {
			return nil, usagef("db expects: gokit db generate")
		}
		return dbGenerateCommand{}, nil
	case "knowledge":
		if len(args) != 2 || args[1] != "check" {
			return nil, usagef("knowledge expects: gokit knowledge check")
		}
		return knowledgeCommand{}, nil
	case "help":
		if len(args) != 1 {
			return nil, usagef("help takes no arguments")
		}
		return helpCommand{}, nil
	}
	return nil, usagef("unknown command %q", args[0])
}

func parseVerify(args []string) (command, error) {
	profile := "full"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--profile":
			if i+1 >= len(args) {
				return nil, usagef("verify: --profile requires a value")
			}
			profile = args[i+1]
			i++
		default:
			return nil, usagef("verify: unexpected argument %q", args[i])
		}
	}
	if _, ok := profiles[profile]; !ok {
		return nil, usagef("verify: unknown profile %q (want %s)", profile, strings.Join(profileNames(), ", "))
	}
	return verifyCommand{profile: profile}, nil
}

func profileNames() []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
