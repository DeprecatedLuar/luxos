// Command luxos routes argv[1] to the matching internal/commands function.
// It is the only place that prints top-level errors or exits
// (implementation-plan.md G10).
package main

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/commands"
	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/links"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

func main() {
	if os.Geteuid() != 0 {
		ensureLuxLink()
	}

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// ensureLuxLink links ~/.local/bin/lux to the running binary (G8). Any
// failure is printed as a warning; it never stops the command.
func ensureLuxLink() {
	p, err := paths.Resolve()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning:", err)
		return
	}

	exe, err := shared.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning:", err)
		return
	}

	state, err := links.EnsureLux(p.LuxLink, exe)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Warning:", err)
		return
	}

	switch state {
	case links.LuxLinkCreated:
		fmt.Printf("Linked %s -> %s\n", p.LuxLink, exe)
	case links.LuxLinkConflict:
		fmt.Fprintf(os.Stderr, "Warning: %s exists and is not a link to %s; left untouched\n", p.LuxLink, exe)
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
	case "module", "modules":
		return commands.Module(rest)
	case "user", "users":
		return commands.User(rest)
	case "shell":
		return commands.Shell(rest)
	default:
		fmt.Fprintln(os.Stderr, "Error: unknown command")
		_ = help.Run(nil)
		os.Exit(1)
		return nil
	}
}
