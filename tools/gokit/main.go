// Command gokit is the repository tooling CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

// os.Exit skips defers, so runMain returns the status.
func main() {
	os.Exit(runMain())
}

func runMain() int {
	cmd, err := parseCommand(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "gokit: "+err.Error())
		var usage *usageError
		if errors.As(err, &usage) {
			fmt.Fprintln(os.Stderr, usageText)
			return 2
		}
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cmd.execute(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "gokit: "+err.Error())
		return 1
	}
	return 0
}
