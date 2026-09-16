// Command luxos routes argv[1] to the matching internal/commands function.
// It is the only place that prints top-level errors or exits
// (implementation-plan.md G10).
package main

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/commands"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		commands.Help(os.Stdout)
		return nil
	}

	cmd, rest := args[0], args[1:]

	switch cmd {
	case "help", "-h", "--help":
		commands.Help(os.Stdout)
		return nil
	case "rebuild":
		return commands.Rebuild(rest)
	default:
		commands.Help(os.Stderr)
		return fmt.Errorf("unknown command '%s'", cmd)
	}
}
