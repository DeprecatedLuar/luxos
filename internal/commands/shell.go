package commands

import (
	"os/exec"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// shellTarget is the soft-shadow target `luxos shell` execs
// (implementation-plan.md G7).
const shellTarget = "nix-shell"

// Shell implements `luxos shell`, a soft shadow that execs nix-shell with
// the given args.
func Shell(args []string) error {
	bin, err := exec.LookPath(shellTarget)
	if err != nil {
		return err
	}
	return nix.Exec(bin, args)
}
