package commands

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/heal"
	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/units"
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

// rebuildActivatingActions are the nixos-rebuild actions that activate the
// built generation; after one succeeds the staged tree is the running system.
var rebuildActivatingActions = map[string]bool{"switch": true, "boot": true, "test": true}

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
const rebuildFlagSpec = "prune:bool machine:value config|C:value backup-dir:value goodbye-luxos:value yes|y:bool"

// modulesFileName is the host folder's selection file.
const modulesFileName = "modules.nix"

// hardwareUnitName is the module name whose absence from the host's
// selection needs confirmation before a rebuild.
const hardwareUnitName = config.HardwareUnitName

// yesFlag is the argument appended when a prompt was already answered, so
// the root process does not ask again.
const yesFlag = "--yes"

// goodbyeNixExt marks the entries a replacement configuration folder must
// contain at least one of.
const goodbyeNixExt = ".nix"

// goodbyeFarewell is printed after a successful goodbye build.
const goodbyeFarewell = "SEE YOU NIX COWBOY..."

// ansiItalic starts italic text.
const ansiItalic = "\x1b[3m"

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

// backupDirEnv is the environment variable paths.Resolve honors as the
// override for the directory adoption moves strangers into.
const backupDirEnv = "LUXOS_BACKUP_DIR"

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

// hardwareSelected reports whether the host's modules.nix selects the
// hardware unit.
func hardwareSelected(hostDir string) (bool, error) {
	list, err := imports.List(filepath.Join(hostDir, modulesFileName))
	if err != nil {
		return false, err
	}
	for _, path := range list {
		if units.NameFromPath(path) == hardwareUnitName {
			return true, nil
		}
	}
	return false, nil
}

// hardwareGuard asks for confirmation when host does not select the
// hardware unit. It reports whether a prompt was answered yes.
func hardwareGuard(hostDir, host string, yes bool) (bool, error) {
	selected, err := hardwareSelected(hostDir)
	if err != nil {
		return false, err
	}
	if selected || yes {
		return false, nil
	}
	ok, err := shared.ConfirmHardwareOff()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("module 'hardware-support' is not enabled on %s: aborted, nothing changed\n  enable it: luxos module enable hardware-support\n  build without it: luxos rebuild --yes <same args>", host)
	}
	return true, nil
}

// Rebuild implements `luxos rebuild`: confirmations first, then escalation
// to root, then heal.Run before exec'ing the real nixos-rebuild.
func Rebuild(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		return help.Run([]string{"help", "rebuild"})
	}

	opts, rest, err := shared.ParsePassthrough(rebuildFlagSpec, args)
	if err != nil {
		return err
	}

	prune := opts["prune"] != ""
	yes := opts["yes"] != ""

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

	if opts["backup-dir"] != "" {
		abs, err := filepath.Abs(opts["backup-dir"])
		if err != nil {
			return err
		}
		if err := os.Setenv(backupDirEnv, abs); err != nil {
			return err
		}
	}

	if opts["goodbye-luxos"] != "" {
		proceed, answered, err := goodbyeConfirm(opts["goodbye-luxos"], yes)
		if err != nil || !proceed {
			return err
		}
		if err := shared.EnsureRoot(escalationArgs(args, answered)); err != nil {
			return err
		}
		return goodbye(opts["goodbye-luxos"], rest)
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

	hostDir, err := config.ResolveHost(p.Machines, host)
	if err != nil {
		return err
	}

	answered, err := hardwareGuard(hostDir, host, yes)
	if err != nil {
		return err
	}

	if err := shared.EnsureRoot(escalationArgs(args, answered)); err != nil {
		return err
	}

	if err := selfUpdate(filepath.Join(hostDir, flakeLockName), args); err != nil {
		return err
	}

	printLogo()

	return stagedRebuild(p, host, prune, rest)
}

// rebuildAction is the nixos-rebuild action in rest: its first element that
// is not a flag, or "" when there is none.
func rebuildAction(rest []string) string {
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// stagedRebuild runs heal and nixos-rebuild while /etc/nixos may change. The
// stage from before is saved in p.PreviousStage and put back unless an
// activating action succeeded, so /etc/nixos always matches the running system.
func stagedRebuild(p paths.Paths, host string, prune bool, rest []string) error {
	if err := staging.RestorePrevious(p.Staging, p.PreviousStage); err != nil {
		return fmt.Errorf("restore %s: %w", p.PreviousStage, err)
	}
	if err := staging.SavePrevious(p.Staging, p.PreviousStage); err != nil {
		return fmt.Errorf("save %s: %w", p.PreviousStage, err)
	}

	// Catch, never ignore: an ignored disposition is inherited by the child.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	runErr := runStaged(p, host, prune, rest)

	if runErr == nil && rebuildActivatingActions[rebuildAction(rest)] {
		if err := staging.DropPrevious(p.PreviousStage); err != nil {
			return fmt.Errorf("drop %s: %w", p.PreviousStage, err)
		}
		return nil
	}
	if err := staging.RestorePrevious(p.Staging, p.PreviousStage); err != nil {
		return fmt.Errorf("restore %s: %w", p.PreviousStage, err)
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return shared.ExitCode(ee.ExitCode())
	}
	return runErr
}

// runStaged heals, stages and runs nixos-rebuild as a child.
func runStaged(p paths.Paths, host string, prune bool, rest []string) error {
	if err := heal.Run(os.Stdout, p, host, prune); err != nil {
		return err
	}

	rebuildBin, err := nix.RebuildFromFlake(p.Staging, host)
	if err != nil {
		return err
	}

	flakeArgs := []string{"--flake", p.Staging + "#" + host, "--no-write-lock-file"}
	flakeArgs = append(flakeArgs, rest...)

	return nix.Run(rebuildBin, flakeArgs)
}

// escalationArgs is the rebuild command line handed to the root process,
// with --yes appended when a prompt was already answered yes.
func escalationArgs(args []string, answered bool) []string {
	out := append([]string{"rebuild"}, args...)
	if answered {
		out = append(out, yesFlag)
	}
	return out
}

// goodbyeConfirm validates dir, lists what will be removed and asks for
// confirmation unless yes. proceed is false when the answer was no;
// answered is true when a prompt was answered yes.
func goodbyeConfirm(dir string, yes bool) (proceed, answered bool, err error) {
	p, err := paths.Resolve()
	if err != nil {
		return false, false, err
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return false, false, err
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return false, false, fmt.Errorf("--goodbye-luxos: %s does not exist or is not a directory", abs)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return false, false, err
	}
	hasNix := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), goodbyeNixExt) {
			hasNix = true
			break
		}
	}
	if !hasNix {
		return false, false, fmt.Errorf("--goodbye-luxos: %s contains no %s entry", abs, goodbyeNixExt)
	}

	fmt.Printf("The contents of %s will replace %s verbatim.\n", abs, p.Staging)
	fmt.Println("These entries will be removed from " + p.Staging + ":")
	for _, name := range staging.Owned() {
		fmt.Println("  " + name)
	}
	if yes {
		return true, false, nil
	}
	ok, err := shared.Confirm("Proceed? [y/N] ", false)
	if err != nil {
		return false, false, err
	}
	if !ok {
		fmt.Println("Nothing changed.")
		return false, false, nil
	}
	return true, true, nil
}

// goodbye replaces /etc/nixos with the folder dir, then builds it with a
// channel-built nixos-rebuild as a child process so the result is known.
func goodbye(dir string, rest []string) error {
	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	if err := staging.Replace(abs, p.Staging); err != nil {
		return err
	}

	bin, err := nix.RebuildFromChannel()
	if err != nil {
		return err
	}
	if err := nix.Run(bin, rest); err != nil {
		return err
	}

	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}
	msg := goodbyeFarewell
	if colorsEnabled(tty) {
		msg = ansiItalic + goodbyeFarewell + colorReset
	}
	fmt.Print("\n\n" + msg + "\n")
	return nil
}
