package commands

import (
	"errors"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/shell"
)

// DeadlockFlag is accepted by every command and stripped before dispatch.
const DeadlockFlag = "--deadlock"

var errDeadlockNoInput = errors.New("--deadlock: the host's flake has no luxos input")

// Deadlock updates the host's luxos input, then execs args as that luxos:
// for when the installed luxos is too old to reach the next rebuild.
func Deadlock(args []string) error {
	_, _, hostDir, err := shared.ResolveHost(nil)
	if err != nil {
		return err
	}
	if err := updateInputs(nil, []string{nix.LuxosInput}); err != nil {
		return err
	}

	ref, ok, err := pinnedRef(filepath.Join(hostDir, flakeLockName))
	if err != nil {
		return err
	}
	if !ok {
		return errDeadlockNoInput
	}
	bin, err := buildPinned(ref)
	if err != nil {
		return err
	}
	return shell.Exec(bin, args)
}
