package commands

import (
	"os/exec"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

const shellTarget = "nix-shell"

func Shell(args []string) error {
	bin, err := exec.LookPath(shellTarget)
	if err != nil {
		return err
	}
	return nix.Exec(bin, args)
}
