package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands"
	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var code shared.ExitCode
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

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
	case "version", "-v", "--version":
		return commands.Version(rest)
	case "rebuild":
		return commands.Rebuild(rest)
	case "flake":
		return commands.Flake(rest)
	case "flakes":
		return commands.Flake(withDefaultVerb(rest, "list"))
	case "module":
		return commands.Module(rest)
	case "modules", "m":
		return commands.Module(withDefaultVerb(rest, "list"))
	case "list", "ls", "edit", "enable", "disable", "remove", "rm", "rename", "rn":
		return commands.Module(args)
	case "user":
		return commands.User(rest)
	case "users":
		return commands.User(withDefaultVerb(rest, "list"))
	case "shell":
		return commands.Shell(rest)
	default:
		fmt.Fprintln(os.Stderr, "Error: unknown command")
		_ = help.Run(nil)
		return shared.ExitCode(1)
	}
}
