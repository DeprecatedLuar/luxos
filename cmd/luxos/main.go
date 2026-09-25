// Command luxos routes argv[1] to the matching internal/commands function.
// It is the only place that prints top-level errors or exits
// (implementation-plan.md G10).
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands"
	"github.com/DeprecatedLuar/luxos/internal/commands/help"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// withDefaultVerb prepends verb to args unless args already starts with a
// verb — i.e. unless args is non-empty and its first element isn't a flag.
// It lets `luxos modules`/`luxos users` shortcuts accept flags in place of
// an explicit "list" ("luxos users --flat") the same way they accept none.
func withDefaultVerb(args []string, verb string) []string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args
	}
	return append([]string{verb}, args...)
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
	case "flake":
		return commands.Flake(rest)
	case "module":
		return commands.Module(rest)
	case "modules":
		return commands.Module(withDefaultVerb(rest, "list"))
	case "list", "ls":
		return commands.Module(append([]string{"list"}, rest...))
	case "edit":
		return commands.Module(append([]string{"edit"}, rest...))
	case "enable":
		return commands.Module(append([]string{"enable"}, rest...))
	case "disable":
		return commands.Module(append([]string{"disable"}, rest...))
	case "remove", "rm":
		return commands.Module(append([]string{"remove"}, rest...))
	case "rename", "rn":
		return commands.Module(append([]string{"rename"}, rest...))
	case "user":
		return commands.User(rest)
	case "users":
		return commands.User(withDefaultVerb(rest, "list"))
	case "shell":
		return commands.Shell(rest)
	default:
		fmt.Fprintln(os.Stderr, "Error: unknown command")
		_ = help.Run(nil)
		os.Exit(1)
		return nil
	}
}
