package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/heal"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
)

// rebuildLogoLines is the ASCII-art logo printed at the start of every
// rebuild, ported verbatim from bin/lib/nixos-rebuild/main.sh's
// REBUILD_HEADER.
var rebuildLogoLines = []string{
	"██╗     ██╗   ██╗██╗  ██╗ ██████╗ ███████╗",
	"██║     ██║   ██║╚██╗██╔╝██╔═══██╗██╔════╝",
	"██║     ██║   ██║ ╚███╔╝ ██║   ██║███████╗",
	"██║     ██║   ██║ ██╔██╗ ██║   ██║╚════██║",
	"███████╗╚██████╔╝██╔╝ ██╗╚██████╔╝███████║",
	"╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝",
}

// rebuildFooter is the logo's signature line.
const rebuildFooter = "                          made by me <3 (luar)"

// rebuildHeader is the plain (non-color) header, for a non-TTY or
// NO_COLOR — the logo lines and footer, unchanged from the original.
const rebuildHeader = "\n" +
	"██╗     ██╗   ██╗██╗  ██╗ ██████╗ ███████╗\n" +
	"██║     ██║   ██║╚██╗██╔╝██╔═══██╗██╔════╝\n" +
	"██║     ██║   ██║ ╚███╔╝ ██║   ██║███████╗\n" +
	"██║     ██║   ██║ ██╔██╗ ██║   ██║╚════██║\n" +
	"███████╗╚██████╔╝██╔╝ ██╗╚██████╔╝███████║\n" +
	"╚══════╝ ╚═════╝ ╚═╝  ╚═╝ ╚═════╝ ╚══════╝\n" +
	rebuildFooter + "\n\n"

// rebuildLogoFrom/To are the gradient's endpoints — green (#CCF391) to
// purple (#B5A6FA), matching module list's palette.
var (
	rebuildLogoFrom = [3]int{0xCC, 0xF3, 0x91}
	rebuildLogoTo   = [3]int{0xB5, 0xA6, 0xFA}
)

// gradientLogo renders rebuildLogoLines as a flat-per-row, top-to-bottom
// green-to-purple gradient, footer in the tree's connector tone.
func gradientLogo() string {
	rows := len(rebuildLogoLines)

	var b strings.Builder
	b.WriteByte('\n')
	for y, line := range rebuildLogoLines {
		t := y
		span := rows - 1
		if span <= 0 {
			span = 1
		}
		r := rebuildLogoFrom[0] + (rebuildLogoTo[0]-rebuildLogoFrom[0])*t/span
		g := rebuildLogoFrom[1] + (rebuildLogoTo[1]-rebuildLogoFrom[1])*t/span
		bl := rebuildLogoFrom[2] + (rebuildLogoTo[2]-rebuildLogoFrom[2])*t/span
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%s", r, g, bl, line)
		b.WriteString(colorReset + "\n")
	}
	b.WriteString(colorLine + rebuildFooter + colorReset + "\n\n")
	return b.String()
}

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

	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	if colorsEnabled(tty) {
		fmt.Print(gradientLogo())
	} else {
		fmt.Print(rebuildHeader)
	}

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
