package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/heal"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
)

// flakeFlagSpec is the flag spec passed to shared.Parse.
const flakeFlagSpec = "machine:value config|C:value"

// Flake implements `luxos flake`; update is its only verb.
func Flake(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", "flake"})
	}

	switch args[0] {
	case "update":
		return flakeUpdate(args[1:])
	default:
		return help.Run([]string{"help", "flake"})
	}
}

// flakeUpdate escalates to root, stages the host's tree, runs
// `nix flake update` on it and copies the lock back to the host folder.
func flakeUpdate(args []string) error {
	if err := shared.EnsureRoot(append([]string{"flake", "update"}, args...)); err != nil {
		return err
	}

	opts, names, err := shared.Parse(flakeFlagSpec, args)
	if err != nil {
		return err
	}

	if opts["config"] != "" {
		abs, err := filepath.Abs(opts["config"])
		if err != nil {
			return err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return fmt.Errorf("config dir %s does not exist", abs)
		}
		if err := os.Setenv(configDirEnv, abs); err != nil {
			return err
		}
	}

	host := opts["machine"]
	if host == "" {
		host, err = os.Hostname()
		if err != nil {
			return err
		}
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	hostDir, err := config.ResolveHost(p.Local, host)
	if err != nil {
		return err
	}

	if err := heal.Run(os.Stdout, p, host, false); err != nil {
		return err
	}

	if err := nix.FlakeUpdate(p.Staging, names...); err != nil {
		return err
	}

	hostLock := filepath.Join(hostDir, flakeLockName)
	if err := staging.CopyLockBack(p.Staging, hostLock); err != nil {
		return err
	}

	fmt.Printf("  updated: %s\n", hostLock)
	return nil
}
