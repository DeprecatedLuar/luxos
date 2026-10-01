// Package heal runs before every rebuild, sequencing config, imports, generate,
// units, refs, hardware and staging, and printing progress to the given
// io.Writer. No parsing or decision logic: each package takes explicit
// directories, returns values plus error.
package heal

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

// Nix invocations, replaceable so tests need neither network nor flake-file.
var (
	writeFlake = nix.WriteFlake
	flakeLock  = nix.FlakeLock
)

// selectedInputs returns the flake-file.inputs declarations of the host's
// machine.nix and of every module it builds.
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

// Run performs the full self-heal sequence for host: ensure .gitignore,
// ensure global environment file, sync framework modules, validate and protect
// the host folder, ensure this computer's hardware folder and the host's link
// to it, heal host entrypoints, validate module boundaries, adopt /etc/nixos,
// materialize staging, generate+install framework/flake-file.nix, detect GPUs
// and install framework/gpu.nix, then flake.nix via flake-file, and
// framework/configuration.nix. prune, when true, removes unresolvable import
// lines from the active host's entrypoint instead of erroring.
func Run(w io.Writer, p paths.Paths, host string, prune bool) error {
	hostDir := filepath.Join(p.Machines, host)

	if err := config.Ensure(w, p, host); err != nil {
		return err
	}
	envPath := filepath.Join(p.Config, config.EnvironmentFile)

	// 5. heal imports
	fmt.Fprintf(w, "Healing modules imports...\n")
	healChanges, healWarnings, err := modules.Heal(p.Machines, p.Modules, host, prune)
	for _, c := range healChanges {
		fmt.Fprintf(w, "%s\n", formatImportChange(c))
	}
	for _, wm := range healWarnings {
		fmt.Fprintf(w, "Warning: %s\n", wm)
	}
	if err != nil {
		return err
	}

	// 6. validate module boundaries
	fmt.Fprintf(w, "Validating module boundaries...\n")
	h, err := modules.Load(p.Modules, hostDir)
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

	// 7a. adopt the flake root: move strangers out of it
	fmt.Fprintf(w, "Adopting %s...\n", p.Staging)
	adoptChanges, err := staging.Adopt(p.Staging, p.Backup)
	if errors.Is(err, staging.ErrNoBackupDir) {
		return fmt.Errorf("%s holds entries luxos does not own and there is no user to move them to; choose a directory with:\n  luxos rebuild --backup-dir <path>", p.Staging)
	}
	if err != nil {
		return err
	}
	for _, c := range adoptChanges {
		if c.Kind == staging.ChangeMoved {
			fmt.Fprintf(w, "  moved: %s -> %s\n", c.Path, c.Dest)
		} else {
			fmt.Fprintf(w, "  converted %s from a symlink to a real directory\n", c.Path)
		}
	}

	// the bootstrap flake's inputs come from the selected modules' own
	// flake-file.inputs declarations (plus machine.nix's base channel), so a
	// module that imports from its own input (a nixos-hardware module, say)
	// already has that input by the time write-flake evaluates it.
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
	for _, g := range gpus {
		fmt.Fprintf(w, "  %s %s: %s\n", g.Vendor, g.Class, g.BusID)
	}

	fmt.Fprintf(w, "Materializing %s for %s...\n", p.Staging, host)
	if err := staging.Materialize(p.Staging, h, hwDir, envPath, inputs, gpus); err != nil {
		return err
	}

	// let flake-file write flake.nix, then lock
	fmt.Fprintf(w, "Generating flake.nix with flake-file...\n")
	if err := writeFlake(p.Staging); err != nil {
		return err
	}
	if err := flakeLock(p.Staging); err != nil {
		return err
	}
	hostLock := filepath.Join(hostDir, "flake.lock")
	changed, err := config.CopyLockBack(filepath.Join(p.Staging, "flake.lock"), hostLock)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(w, "  locked new inputs: %s\n", hostLock)
	}

	// 11. make the generated tree read-only
	fmt.Fprintf(w, "Sealing %s...\n", p.Staging)
	return staging.Seal(p.Staging)
}

func formatImportChange(c modules.Change) string {
	if c.New == "" {
		return fmt.Sprintf("%s: removed ./%s", c.File, c.Old)
	}
	return fmt.Sprintf("%s: ./%s -> ./%s", c.File, c.Old, c.New)
}
