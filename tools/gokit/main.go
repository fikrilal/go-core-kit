// Command gokit is the repository tooling CLI.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gokit: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 2 && args[0] == "db" && args[1] == "generate" {
		return generateDB()
	}
	return errors.New("usage: gokit db generate")
}
