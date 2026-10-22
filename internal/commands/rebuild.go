package commands

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/setup"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/shell"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// After one succeeds the staged tree is the running system.
var rebuildActivatingActions = map[string]bool{"switch": true, "boot": true, "test": true}

const rebuildFlagSpec = "prune:bool host:value config|C:value backup-dir:value goodbye-luxos:value yes|y:bool"

// Appended when a prompt was already answered, so the root process does not ask again.
const yesFlag = "--yes"

// Appended when setup picked the host, so the root process builds it.
const hostFlag = "--host"

// A replacement configuration folder must contain at least one entry with this extension.
const goodbyeNixExt = ".nix"

const goodbyeFarewell = "SEE YOU NIX COWBOY..."

const flakeLockName = "flake.lock"

// What the spinner line becomes once staging succeeds.
const prebuildLabel = "prebuild"

// A binary installed by a generation; anything else is hand-built and must never replace itself.
const nixStorePrefix = "/nix/store/"

const luxosBinRelpath = "bin/luxos"

// The only flake.lock node type the self-update understands.
const githubLockType = "github"

const selfUpdateNotice = "luxos updated, rebuilding with the new version"

func printLogo() {
	ui.Print(os.Stdout, ui.Logo(os.Stdout))
}

// Re-executes this process as the luxos revision pinned in hostLock when the
// running binary is not already that revision. Returns nil when no swap is needed
// and does not return when one happens.
func selfUpdate(hostLock string, args []string) error {
	ref, ok, err := pinnedRef(hostLock)
	if err != nil || !ok {
		return err
	}

	exe, err := shell.Self()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(exe, nixStorePrefix) {
		return nil
	}

	pinned, err := buildPinned(ref)
	if err != nil {
		return err
	}
	if pinned == exe {
		return nil
	}

	ui.Done(os.Stdout, selfUpdateNotice)
	return shell.Exec(pinned, append([]string{"rebuild"}, args...))
}

// The flake ref of the luxos revision pinned in hostLock; ok is false when
// the host has no luxos input.
func pinnedRef(hostLock string) (ref string, ok bool, err error) {
	lock, err := nix.ReadLock(hostLock)
	if err != nil {
		return "", false, err
	}
	node, ok := lock.Input(nix.LuxosInput)
	if !ok {
		return "", false, nil
	}
	in := node.Locked
	if in.Type != githubLockType {
		return "", false, fmt.Errorf("flake input %q has lock type %q; the self-update only understands %s inputs", nix.LuxosInput, in.Type, githubLockType)
	}
	return fmt.Sprintf("github:%s/%s/%s", in.Owner, in.Repo, in.Rev), true, nil
}

// Builds the luxos at ref and returns its binary.
func buildPinned(ref string) (string, error) {
	out, err := nix.BuildFlakeRef(ref)
	if err != nil {
		return "", fmt.Errorf("build pinned luxos %s: %w", ref, err)
	}
	return filepath.Join(out, luxosBinRelpath), nil
}

// bootGuard asks for confirmation when no selection of the host at hostDir
// selects this computer's boot unit. Before the first rebuild there is no
// machine link and the seeded selection selects it, so nothing is asked.
// Reports whether a prompt was answered yes.
func bootGuard(p paths.Paths, hostDir string, yes bool) (bool, error) {
	machineDir, err := config.LinkedMachineDir(p.Modules)
	if errors.Is(err, config.ErrNoMachineLink) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	h, err := modules.Load(p.Modules, hostDir, machineDir)
	if err != nil {
		return false, err
	}
	if h.BootSelected() || yes {
		return false, nil
	}
	ok, err := shared.ConfirmBootOff()
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("module 'boot' is not selected in modules/machine/default.nix: aborted, nothing changed\n  enable it: luxos module enable boot\n  build without it: luxos rebuild --yes <same args>")
	}
	return true, nil
}

const rebuildCommand = "rebuild"

func Rebuild(args []string) error {
	if len(args) > 0 && isHelpFlag(args[0]) {
		return help.Command(os.Stdout, rebuildCommand)
	}

	opts, rest, err := shared.ParsePassthrough(rebuildFlagSpec, args)
	if err != nil {
		return err
	}

	prune := opts["prune"] != ""
	yes := opts["yes"] != ""

	if opts["backup-dir"] != "" {
		abs, err := filepath.Abs(opts["backup-dir"])
		if err != nil {
			return err
		}
		if err := os.Setenv(paths.BackupDirEnv, abs); err != nil {
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

	target, host, err := shared.Target(opts)
	if err != nil {
		return err
	}
	state, err := config.ConfigState(target.Config, target.Hosts, host)
	if err != nil {
		return err
	}
	if state == config.StateNoConfig && opts["host"] == "" {
		name, err := setup.Launch(ui.NewPrompter(os.Stdin, os.Stdout), os.Stdout, target, host, state)
		if err != nil {
			return err
		}
		args = withHost(opts, args, name)
	}

	p, host, hostDir, err := shared.ResolveHost(opts)
	if err != nil {
		return err
	}

	answered, err := bootGuard(p, hostDir, yes)
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

// The nixos-rebuild action in rest: its first element that is not a flag, or "" when there is none.
func rebuildAction(rest []string) string {
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// Builds host's stage in p.NewStage and promotes it to p.Staging only after an
// activating action succeeds, so /etc/nixos only ever holds a stage that
// built and activated. Signals stay caught until it returns, so an interrupt
// cannot land between the two renames of a promotion.
func stagedRebuild(p paths.Paths, host string, prune bool, rest []string) error {
	if err := staging.Recover(p.Staging, p.NewStage, p.OldStage); err != nil {
		return fmt.Errorf("recover %s: %w", p.Staging, err)
	}

	// Catch, never ignore: an ignored disposition is inherited by the child.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	runErr := runStaged(p, host, prune, rest, sigs)

	if runErr == nil && rebuildActivatingActions[rebuildAction(rest)] {
		return promote(p)
	}
	if err := os.RemoveAll(p.NewStage); err != nil {
		return errors.Join(runErr, fmt.Errorf("remove %s: %w", p.NewStage, err))
	}
	return runErr
}

// promote moves the entries luxos does not own out of p.Staging, then swaps
// p.NewStage in for it.
func promote(p paths.Paths) error {
	out := ui.NewProgress(os.Stderr)
	if err := adoptStaging(out, p); err != nil {
		return err
	}
	if err := staging.Promote(p.NewStage, p.Staging, p.OldStage); err != nil {
		return fmt.Errorf("promote %s to %s: %w", p.NewStage, p.Staging, err)
	}
	return out.Err()
}

// adoptStaging moves every entry of p.Staging luxos does not own into p.Backup.
func adoptStaging(out *ui.Progress, p paths.Paths) error {
	out.Printf("Adopting %s...\n", p.Staging)
	adopted, err := staging.Adopt(p.Staging, p.Backup)
	if errors.Is(err, staging.ErrNoBackupDir) {
		return fmt.Errorf("%s holds entries luxos does not own and there is no user to move them to; choose a directory with:\n  luxos rebuild --backup-dir <path>", p.Staging)
	}
	if err != nil {
		return err
	}
	printAdopted(out, adopted)
	return out.Err()
}

func printAdopted(out *ui.Progress, changes []staging.Change) {
	for _, c := range changes {
		if c.Kind == staging.ChangeMoved {
			out.Changef("  moved: %s -> %s", c.Path, c.Dest)
		} else {
			out.Changef("  converted %s from a symlink to a real directory", c.Path)
		}
	}
}

func runStaged(p paths.Paths, host string, prune bool, rest []string, sigs <-chan os.Signal) error {
	out := ui.NewProgress(os.Stderr)
	out.Start()
	defer out.Stop()
	h, err := prepare(out, p, host, prune)
	if err != nil {
		return err
	}
	if err := stage(out, p, h, p.NewStage, realFlakeSteps); err != nil {
		return err
	}
	out.Printf("Sealing %s...\n", p.NewStage)
	if err := staging.Seal(p.NewStage); err != nil {
		return err
	}
	if err := interrupted(sigs); err != nil {
		return err
	}
	if err := checkAssertions(out, p.NewStage, host); err != nil {
		return err
	}
	out.Done(prebuildLabel)

	rebuildBin, err := nix.RebuildFromFlake(p.NewStage, host)
	if err != nil {
		return err
	}

	flakeArgs := []string{"--flake", p.NewStage + "#" + host, "--no-write-lock-file"}
	flakeArgs = append(flakeArgs, rest...)

	// Only nixos-rebuild's own exit status becomes the command's: it has
	// already printed its error. Any other failure keeps its message.
	err = shell.Run(shell.Cmd{Bin: rebuildBin, Args: flakeArgs})
	if code, ok := shell.ExitCode(err); ok {
		return shared.ExitCode(code)
	}
	return err
}

// interrupted reports a SIGINT or SIGTERM received while Go-side staging ran.
func interrupted(sigs <-chan os.Signal) error {
	select {
	case s := <-sigs:
		return fmt.Errorf("interrupted (%s) before nixos-rebuild started; nothing was promoted", s)
	default:
		return nil
	}
}

// The rebuild command line handed to the root process, with --yes appended
// when a prompt was already answered yes.
// withHost makes the rest of the run, and its sudo re-exec, build name.
func withHost(opts map[string]string, args []string, name string) []string {
	opts["host"] = name
	return append(args, hostFlag, name)
}

func escalationArgs(args []string, answered bool) []string {
	out := append([]string{"rebuild"}, args...)
	if answered {
		out = append(out, yesFlag)
	}
	return out
}

// Validates dir, lists what will be removed and asks for confirmation unless yes.
// proceed is false when the answer was no; answered is true when a prompt was answered yes.
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

	ui.Text(os.Stdout, fmt.Sprintf("The contents of %s will replace %s verbatim.", abs, p.Staging))
	ui.Text(os.Stdout, "These entries will be removed from "+p.Staging+":")
	for _, name := range staging.Owned() {
		ui.Text(os.Stdout, "  "+name)
	}
	if yes {
		return true, false, nil
	}
	ok, err := ui.Confirm("Proceed? [y/N] ", false)
	if err != nil {
		return false, false, err
	}
	if !ok {
		ui.Text(os.Stdout, "Nothing changed.")
		return false, false, nil
	}
	return true, true, nil
}

// Replaces /etc/nixos with the folder dir, then builds it with a channel-built
// nixos-rebuild as a child process so the result is known.
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
	if err := shell.Run(shell.Cmd{Bin: bin, Args: rest}); err != nil {
		return err
	}

	ui.Print(os.Stdout, "\n\n"+ui.Italic(os.Stdout, goodbyeFarewell)+"\n")
	return nil
}
