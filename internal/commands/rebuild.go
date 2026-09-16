package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/heal"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
)

// rebuildHeader is printed at the start of every rebuild, ported verbatim
// from bin/lib/nixos-rebuild/main.sh's REBUILD_HEADER.
const rebuildHeader = `
██╗     ██╗   ██╗██╗  ██╗ ██████╗ ███████╗
██║     ██║   ██║╚██╗██╔╝██╔═══██╗██╔════╝
██║     ██║   ██║ ╚███╔╝ ██║   ██║███████╗
██║     ██║   ██║ ██╔██╗ ██║   ██║╚════██║
███████╗╚██████╔╝██╔╝ ██╗╚██████╔╝███████║
╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝
                          made by me <3 (luar)
`

// rebuildFlagSpec is the flag spec passed to shared.ParsePassthrough, ported
// from rebuild::run in bin/lib/nixos-rebuild/main.sh.
const rebuildFlagSpec = "bypass:bool update-lock:bool prune:bool machine:value"

// flakeLockName is channels.toml's sibling in Paths.Config.
const flakeLockName = "flake.lock"

// Rebuild implements `luxos rebuild`, escalating to root and delegating to
// heal.Run before exec'ing the real nixos-rebuild.
func Rebuild(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		return help.Run([]string{"help", "rebuild"})
	}

	if err := shared.EnsureRoot(append([]string{"rebuild"}, args...)); err != nil {
		return err
	}

	fmt.Print(rebuildHeader)

	opts, rest, err := shared.ParsePassthrough(rebuildFlagSpec, args)
	if err != nil {
		return err
	}

	bypass := opts["bypass"] != ""
	updateLock := opts["update-lock"] != ""
	prune := opts["prune"] != ""

	if bypass {
		if opts["machine"] != "" {
			return fmt.Errorf("--bypass and --machine cannot be combined - --bypass never reads .local")
		}
		bin, err := nix.RebuildFromChannel()
		if err != nil {
			return err
		}
		return nix.Exec(bin, rest)
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

	if _, err := config.ResolveMachine(p.Local, host); err != nil {
		return err
	}

	exe, err := shared.Executable()
	if err != nil {
		return err
	}

	if err := heal.Run(os.Stdout, p, host, exe, prune); err != nil {
		return err
	}

	if updateLock {
		uid := envInt("SUDO_UID")
		gid := envInt("SUDO_GID")
		configLock := filepath.Join(p.Config, flakeLockName)
		if err := staging.UpdateLock(p.Staging, configLock, uid, gid); err != nil {
			return err
		}
	}

	rebuildBin, err := nix.RebuildFromFlake(p.Staging, host)
	if err != nil {
		return err
	}

	flakeArgs := []string{"--flake", p.Staging + "#" + host}
	if !updateLock {
		flakeArgs = append(flakeArgs, "--no-write-lock-file")
	}
	flakeArgs = append(flakeArgs, rest...)

	return nix.Exec(rebuildBin, flakeArgs)
}

// envInt reads an environment variable as an int, falling back to 0 when
// unset or unparsable.
func envInt(name string) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err != nil {
		return 0
	}
	return v
}
