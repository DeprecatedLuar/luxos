package commands

import "github.com/DeprecatedLuar/luxos/internal/shell"

const shellTarget = "nix-shell"

func Shell(args []string) error {
	return shell.Exec(shellTarget, args)
}
