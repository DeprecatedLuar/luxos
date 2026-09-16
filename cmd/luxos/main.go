// Command luxos routes argv[1] to the matching internal/commands function.
// It is the only place that prints top-level errors or exits
// (implementation-plan.md G10).
package main

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/commands"
	"github.com/DeprecatedLuar/luxos/internal/commands/help"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return help.Run(nil)
	}

	cmd, rest := args[0], args[1:]

	switch cmd {
	case "help", "-h", "--help":
		return help.Run(args)
	case "rebuild":
		return commands.Rebuild(rest)
	case "module":
		return commands.Module(rest)
	case "user":
		return commands.User(rest)
	default:
		fmt.Fprintln(os.Stderr, "Error: unknown command")
		_ = help.Run(nil)
		os.Exit(1)
		return nil
	}
}
