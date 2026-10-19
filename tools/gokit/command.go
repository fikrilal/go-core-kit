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
  db export --out <file>          write the compiled lane's tables as a snapshot
  db import --in <file> [--replace] [--dry-run]
                                  apply a snapshot to the compiled lane
  drift check                     regenerate and fail on uncommitted or stale artifacts
  contract sync|check|generate    pin the backend snapshot; verify digest; regenerate types
  gates honesty                   prove the verification gates fail on injected violations
  knowledge check                 env.example, project map, links, ADR index
  scaffold feature --name <name>  create a feature skeleton and wire the API
  scaffold endpoint --feature <f> --operation <id>
                                  implement one allowlisted snapshot operation
  remove feature --name <name>    delete a feature and unwire it
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

type contractSyncCommand struct {
	source string
}

func (c contractSyncCommand) execute(ctx context.Context) error {
	return runContractSync(ctx, c.source)
}

type contractCheckCommand struct{}

func (contractCheckCommand) execute(context.Context) error {
	return runContractCheck()
}

type contractGenerateCommand struct{}

func (contractGenerateCommand) execute(ctx context.Context) error {
	return runContractGenerate(ctx)
}

type gatesHonestyCommand struct{}

func (gatesHonestyCommand) execute(ctx context.Context) error {
	return runGatesHonesty(ctx)
}

type driftCheckCommand struct{}

func (driftCheckCommand) execute(ctx context.Context) error {
	return runDriftCheck(ctx)
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
		return parseDB(args[1:])
	case "knowledge":
		if len(args) != 2 || args[1] != "check" {
			return nil, usagef("knowledge expects: gokit knowledge check")
		}
		return knowledgeCommand{}, nil
	case "contract":
		return parseContract(args[1:])
	case "gates":
		if len(args) != 2 || args[1] != "honesty" {
			return nil, usagef("gates expects: gokit gates honesty")
		}
		return gatesHonestyCommand{}, nil
	case "drift":
		if len(args) != 2 || args[1] != "check" {
			return nil, usagef("drift expects: gokit drift check")
		}
		return driftCheckCommand{}, nil
	case "scaffold":
		return parseScaffold(args[1:])
	case "remove":
		return parseRemove(args[1:])
	case "help":
		if len(args) != 1 {
			return nil, usagef("help takes no arguments")
		}
		return helpCommand{}, nil
	}
	return nil, usagef("unknown command %q", args[0])
}

func parseScaffold(args []string) (command, error) {
	if len(args) == 0 {
		return nil, usagef("scaffold expects: gokit scaffold feature|endpoint")
	}
	switch args[0] {
	case "feature":
		values, err := parseFlags("scaffold feature", args[1:], "--name")
		if err != nil {
			return nil, err
		}
		name := values["--name"]
		if name == "" {
			return nil, usagef("scaffold feature: --name is required")
		}
		return scaffoldFeatureCommand{name: name}, nil
	case "endpoint":
		values, err := parseFlags("scaffold endpoint", args[1:], "--feature", "--operation")
		if err != nil {
			return nil, err
		}
		feature, operation := values["--feature"], values["--operation"]
		if feature == "" || operation == "" {
			return nil, usagef("scaffold endpoint: --feature and --operation are required")
		}
		return scaffoldEndpointCommand{feature: feature, operation: operation}, nil
	}
	return nil, usagef("scaffold expects: gokit scaffold feature|endpoint")
}

func parseDB(args []string) (command, error) {
	if len(args) == 0 {
		return nil, usagef("db expects: gokit db generate|export|import")
	}
	switch args[0] {
	case "generate":
		if len(args) != 1 {
			return nil, usagef("db generate takes no arguments")
		}
		return dbGenerateCommand{}, nil
	case "export":
		values, err := parseFlags("db export", args[1:], "--out")
		if err != nil {
			return nil, err
		}
		out := values["--out"]
		if out == "" {
			return nil, usagef("db export: --out is required")
		}
		return dbExportCommand{out: out}, nil
	case "import":
		replace, dryRun := false, false
		rest := make([]string, 0, len(args))
		for _, arg := range args[1:] {
			switch arg {
			case "--replace":
				replace = true
			case "--dry-run":
				dryRun = true
			default:
				rest = append(rest, arg)
			}
		}
		values, err := parseFlags("db import", rest, "--in")
		if err != nil {
			return nil, err
		}
		in := values["--in"]
		if in == "" {
			return nil, usagef("db import: --in is required")
		}
		return dbImportCommand{in: in, replace: replace, dryRun: dryRun}, nil
	}
	return nil, usagef("db expects: gokit db generate|export|import")
}

func parseRemove(args []string) (command, error) {
	if len(args) == 0 || args[0] != "feature" {
		return nil, usagef("remove expects: gokit remove feature --name <name>")
	}
	values, err := parseFlags("remove feature", args[1:], "--name")
	if err != nil {
		return nil, err
	}
	name := values["--name"]
	if name == "" {
		return nil, usagef("remove feature: --name is required")
	}
	return removeFeatureCommand{name: name}, nil
}

// parseFlags reads the named flags from args, accepting both `--flag value` and
// `--flag=value`, rejecting anything else under the command's name.
func parseFlags(command string, args []string, names ...string) (map[string]string, error) {
	values := map[string]string{}
	for i := 0; i < len(args); i++ {
		matched := false
		for _, name := range names {
			switch {
			case args[i] == name:
				if i+1 >= len(args) {
					return nil, usagef("%s: %s requires a value", command, name)
				}
				values[name] = args[i+1]
				i++
				matched = true
			case strings.HasPrefix(args[i], name+"="):
				values[name] = strings.TrimPrefix(args[i], name+"=")
				matched = true
			}
			if matched {
				break
			}
		}
		if !matched {
			return nil, usagef("%s: unexpected argument %q", command, args[i])
		}
	}
	return values, nil
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

func parseContract(args []string) (command, error) {
	if len(args) == 0 {
		return nil, usagef("contract expects: gokit contract sync|check|generate")
	}
	switch args[0] {
	case "sync":
		source := ""
		for i := 1; i < len(args); i++ {
			switch {
			case args[i] == "--source":
				if i+1 >= len(args) {
					return nil, usagef("contract sync: --source requires a value")
				}
				source = args[i+1]
				i++
			case strings.HasPrefix(args[i], "--source="):
				source = strings.TrimPrefix(args[i], "--source=")
			default:
				return nil, usagef("contract sync: unexpected argument %q", args[i])
			}
		}
		if source == "" {
			return nil, usagef("contract sync: --source is required")
		}
		return contractSyncCommand{source: source}, nil
	case "check":
		if len(args) != 1 {
			return nil, usagef("contract check takes no arguments")
		}
		return contractCheckCommand{}, nil
	case "generate":
		if len(args) != 1 {
			return nil, usagef("contract generate takes no arguments")
		}
		return contractGenerateCommand{}, nil
	}
	return nil, usagef("contract expects: gokit contract sync|check|generate")
}
