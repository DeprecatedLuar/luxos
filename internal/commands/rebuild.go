package commands

import (
	"fmt"
	"os"
	"path/filepath"
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
const rebuildFlagSpec = "bypass:bool prune:bool machine:value config|C:value"

// flakeLockName is the host folder's flake.lock.
const flakeLockName = "flake.lock"

// luxosInputName is the flake input that provides this binary, by convention.
const luxosInputName = "luxos"

// nixStorePrefix marks a binary installed by a generation; anything else is
// a hand-built binary that must never replace itself.
const nixStorePrefix = "/nix/store/"

// luxosBinRelpath is the binary's location inside its derivation output.
const luxosBinRelpath = "bin/luxos"

// githubLockType is the only flake.lock node type the self-update understands.
const githubLockType = "github"

// selfUpdateNotice is printed once, right before the swap.
const selfUpdateNotice = "luxos updated, rebuilding with the new version"

// configDirEnv is the environment variable paths.Resolve honors as the one
// override for CONFIG_DIR.
const configDirEnv = "LUXOS_CONFIG_DIR"

// printLogo prints the rebuild header, colored when stdout allows it.
func printLogo() {
	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	if colorsEnabled(tty) {
		fmt.Print(gradientLogo())
	} else {
		fmt.Print(rebuildHeader)
	}
}

// selfUpdate re-executes this process as the luxos revision pinned in
// hostLock when the running binary is not already that revision. It returns
// nil when no swap is needed and does not return when one happens.
func selfUpdate(hostLock string, args []string) error {
	in, ok, err := staging.ReadLockInput(hostLock, luxosInputName)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if in.Type != githubLockType {
		return fmt.Errorf("flake input %q has lock type %q; the self-update only understands %s inputs", luxosInputName, in.Type, githubLockType)
	}

	exe, err := shared.Executable()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(exe, nixStorePrefix) {
		return nil
	}

	ref := fmt.Sprintf("github:%s/%s/%s", in.Owner, in.Repo, in.Rev)
	out, err := nix.BuildFlakeRef(ref)
	if err != nil {
		return fmt.Errorf("build pinned luxos %s: %w", ref, err)
	}

	pinned := filepath.Join(out, luxosBinRelpath)
	if pinned == exe {
		return nil
	}

	fmt.Println(selfUpdateNotice)
	return nix.Exec(pinned, append([]string{"rebuild"}, args...))
}

// Rebuild implements `luxos rebuild`, escalating to root and delegating to
// heal.Run before exec'ing the real nixos-rebuild.
func Rebuild(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		return help.Run([]string{"help", "rebuild"})
	}

	if err := shared.EnsureRoot(append([]string{"rebuild"}, args...)); err != nil {
		return err
	}

	opts, rest, err := shared.ParsePassthrough(rebuildFlagSpec, args)
	if err != nil {
		return err
	}

	bypass := opts["bypass"] != ""
	prune := opts["prune"] != ""

	if bypass && opts["config"] != "" {
		return fmt.Errorf("--bypass and --config cannot be combined - --bypass never reads the config dir")
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

	if bypass {
		if opts["machine"] != "" {
			return fmt.Errorf("--bypass and --machine cannot be combined - --bypass never reads .local")
		}
		printLogo()
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

	hostDir, err := config.ResolveHost(p.Local, host)
	if err != nil {
		return err
	}

	if err := selfUpdate(filepath.Join(hostDir, flakeLockName), args); err != nil {
		return err
	}

	printLogo()

	exe, err := shared.Executable()
	if err != nil {
		return err
	}

	if err := heal.Run(os.Stdout, p, host, exe, prune); err != nil {
		return err
	}

	rebuildBin, err := nix.RebuildFromFlake(p.Staging, host)
	if err != nil {
		return err
	}

	flakeArgs := []string{"--flake", p.Staging + "#" + host, "--no-write-lock-file"}
	flakeArgs = append(flakeArgs, rest...)

	return nix.Exec(rebuildBin, flakeArgs)
}
