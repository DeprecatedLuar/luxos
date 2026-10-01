package commands

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/hardware"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
)

// flakeSteps are the Nix invocations of a stage, replaceable so tests need
// neither network nor flake-file.
type flakeSteps struct {
	write func(stagingDir string) error // flake-file writes flake.nix
	lock  func(stagingDir string) error // locks missing inputs
}

var realFlakeSteps = flakeSteps{write: nix.WriteFlake, lock: nix.FlakeLock}

// healAndValidate rewrites broken lines of the hosts' selections, then checks
// the active host's module boundaries. prune removes unresolvable lines from
// the active host's selection instead of failing.
func healAndValidate(w io.Writer, p paths.Paths, host string, prune bool) error {
	fmt.Fprintf(w, "Healing modules imports...\n")
	changes, warnings, err := modules.Heal(p.Machines, p.Modules, host, prune)
	for _, c := range changes {
		fmt.Fprintf(w, "%s\n", formatImportChange(c))
	}
	for _, wm := range warnings {
		fmt.Fprintf(w, "Warning: %s\n", wm)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "Validating module boundaries...\n")
	h, err := modules.Load(p.Modules, filepath.Join(p.Machines, host))
	if err != nil {
		return err
	}
	violations, err := h.Validate()
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Fprintf(w, "Error: %s: %s\n", v.File, v.Message)
		}
		return fmt.Errorf("module boundary violations: %d", len(violations))
	}
	return nil
}

func formatImportChange(c modules.Change) string {
	if c.New == "" {
		return fmt.Sprintf("%s: removed ./%s", c.File, c.Old)
	}
	return fmt.Sprintf("%s: ./%s -> ./%s", c.File, c.Old, c.New)
}

// stage writes host's stage into dir from CONFIG_DIR, which it only reads
// (apart from the host's flake.lock, when staging locked something new).
func stage(w io.Writer, p paths.Paths, host, dir string, steps flakeSteps) error {
	hostDir := filepath.Join(p.Machines, host)

	fmt.Fprintf(w, "Adopting %s...\n", dir)
	adopted, err := staging.Adopt(dir, p.Backup)
	if errors.Is(err, staging.ErrNoBackupDir) {
		return fmt.Errorf("%s holds entries luxos does not own and there is no user to move them to; choose a directory with:\n  luxos rebuild --backup-dir <path>", dir)
	}
	if err != nil {
		return err
	}
	printAdopted(w, adopted)

	h, err := modules.Load(p.Modules, hostDir)
	if err != nil {
		return err
	}
	inputs, err := selectedInputs(h)
	if err != nil {
		return err
	}
	hwDir, err := config.HardwareDir(p.Sys, p.HardwareRoot)
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "Detecting GPUs...\n")
	gpus, err := hardware.DetectGPUs(p.Sys)
	if err != nil {
		return err
	}
	printGPUs(w, gpus)

	fmt.Fprintf(w, "Materializing %s for %s...\n", dir, host)
	if err := staging.Materialize(dir, h, hwDir, filepath.Join(p.Config, config.EnvironmentFile), inputs, gpus); err != nil {
		return err
	}

	fmt.Fprintf(w, "Generating flake.nix with flake-file...\n")
	if err := steps.write(dir); err != nil {
		return err
	}
	if err := steps.lock(dir); err != nil {
		return err
	}
	hostLock := filepath.Join(hostDir, flakeLockName)
	changed, err := config.CopyLockBack(filepath.Join(dir, flakeLockName), hostLock)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(w, "  locked new inputs: %s\n", hostLock)
	}

	fmt.Fprintf(w, "Sealing %s...\n", dir)
	return staging.Seal(dir)
}

// selectedInputs returns the flake-file.inputs declarations of the host's
// machine.nix and of every module it builds. They become the bootstrap flake's
// inputs, so a module that imports from its own input (a nixos-hardware
// module, say) already has that input when write-flake evaluates it.
func selectedInputs(h *modules.Host) ([]nix.InputDecl, error) {
	built, err := h.Built()
	if err != nil {
		return nil, err
	}
	files := []string{filepath.Join(h.HostDir, config.MachineFile)}
	for _, m := range built {
		mf, err := m.Files()
		if err != nil {
			return nil, err
		}
		files = append(files, mf...)
	}
	return nix.InputDecls(files...)
}

func printAdopted(w io.Writer, changes []staging.Change) {
	for _, c := range changes {
		if c.Kind == staging.ChangeMoved {
			fmt.Fprintf(w, "  moved: %s -> %s\n", c.Path, c.Dest)
		} else {
			fmt.Fprintf(w, "  converted %s from a symlink to a real directory\n", c.Path)
		}
	}
}

func printGPUs(w io.Writer, gpus []hardware.GPU) {
	for _, g := range gpus {
		fmt.Fprintf(w, "  %s %s: %s\n", g.Vendor, g.Class, g.BusID)
	}
}
