package commands

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/hardware"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/ui"
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
func healAndValidate(out *ui.Progress, p paths.Paths, host string, prune bool) error {
	out.Printf("Healing modules imports...\n")
	changes, warnings, err := modules.Heal(p.Machines, p.Modules, host, prune)
	for _, c := range changes {
		out.Changef("%s", formatImportChange(c))
	}
	for _, wm := range warnings {
		out.Warnf("%s", wm)
	}
	if err != nil {
		return err
	}

	out.Printf("Validating module boundaries...\n")
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
			out.Errf("%s: %s", v.File, v.Message)
		}
		return fmt.Errorf("module boundary violations: %d", len(violations))
	}
	return out.Err()
}

// syncSettings brings host's settings files in line with its selected
// units' options.nix, failing on any unit that breaks a settings rule.
func syncSettings(out *ui.Progress, p paths.Paths, host string) error {
	out.Printf("Syncing module settings...\n")
	h, err := modules.Load(p.Modules, filepath.Join(p.Machines, host))
	if err != nil {
		return err
	}
	rep, err := modules.SyncSettings(h)
	if err != nil {
		return err
	}
	printSettingsReport(out, rep)
	if len(rep.Broken) > 0 {
		for _, u := range rep.Broken {
			for _, problem := range u.Problems {
				out.Errf("%s", problem)
			}
		}
		return fmt.Errorf("module settings: %d unit(s) break the settings rules", len(rep.Broken))
	}
	return out.Err()
}

func printSettingsReport(out *ui.Progress, rep modules.SettingsReport) {
	for _, f := range rep.Created {
		out.Changef("  created %s", f)
	}
	for _, c := range rep.Appended {
		out.Changef("  %s: added %s", c.File, strings.Join(c.Keys, ", "))
	}
	for _, c := range rep.Commented {
		out.Warnf("%s: commented out %s (not declared by the unit)", c.File, strings.Join(c.Keys, ", "))
	}
}

func formatImportChange(c modules.Change) string {
	if c.New == "" {
		return fmt.Sprintf("%s: removed ./%s", c.File, c.Old)
	}
	return fmt.Sprintf("%s: ./%s -> ./%s", c.File, c.Old, c.New)
}

// stage writes host's stage into dir from CONFIG_DIR, which it only reads
// (apart from the host's flake.lock, when staging locked something new).
func stage(out *ui.Progress, p paths.Paths, host, dir string, steps flakeSteps) error {
	hostDir := filepath.Join(p.Machines, host)

	out.Printf("Adopting %s...\n", dir)
	adopted, err := staging.Adopt(dir, p.Backup)
	if errors.Is(err, staging.ErrNoBackupDir) {
		return fmt.Errorf("%s holds entries luxos does not own and there is no user to move them to; choose a directory with:\n  luxos rebuild --backup-dir <path>", dir)
	}
	if err != nil {
		return err
	}
	printAdopted(out, adopted)

	h, err := modules.Load(p.Modules, hostDir)
	if err != nil {
		return err
	}
	hwDir, err := config.HardwareDir(p.Sys, p.HardwareRoot)
	if err != nil {
		return err
	}
	inputs, err := selectedInputs(h, hwDir)
	if err != nil {
		return err
	}

	out.Printf("Detecting hardware...\n")
	facts, err := detectFacts(p.Sys)
	if err != nil {
		return err
	}
	printFacts(out, facts)

	out.Printf("Materializing %s for %s...\n", dir, host)
	if err := staging.Materialize(dir, h, hwDir, filepath.Join(p.Config, config.EnvironmentFile), inputs, facts, h.StagedSettings()); err != nil {
		return err
	}

	out.Printf("Generating flake.nix with flake-file...\n")
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
		out.Changef("  locked new inputs: %s", hostLock)
	}

	out.Printf("Sealing %s...\n", dir)
	if err := staging.Seal(dir); err != nil {
		return err
	}
	return out.Err()
}

// selectedInputs returns the flake-file.inputs declarations of the host's
// machine.nix and of every module it builds. They become the bootstrap flake's
// inputs, so a module that imports from its own input (a nixos-hardware
// module, say) already has that input when write-flake evaluates it. hwDir
// stands in for the hardware-support module on a host without its link.
func selectedInputs(h *modules.Host, hwDir string) ([]nix.InputDecl, error) {
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
	// A host that is not the active one has no hardware-support link, so its
	// selected hardware folder is not among the built modules.
	if _, selected := h.Selected(config.HardwareUnitName); selected {
		if _, linked := h.Find(config.HardwareUnitName); !linked {
			hw := modules.Module{Name: config.HardwareUnitName, Path: config.HardwareUnitPath, Abs: hwDir}
			hf, err := hw.Files()
			if err != nil {
				return nil, err
			}
			files = append(files, hf...)
		}
	}
	return nix.InputDecls(files...)
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

// detectFacts reads every hardware fact the build gets.
func detectFacts(sysDir string) (hardware.Facts, error) {
	gpus, err := hardware.DetectGPUs(sysDir)
	if err != nil {
		return hardware.Facts{}, err
	}
	dmi, err := hardware.DetectDMI(sysDir)
	if err != nil {
		return hardware.Facts{}, err
	}
	profiles, err := hardware.DetectPlatformProfiles(sysDir)
	if err != nil {
		return hardware.Facts{}, err
	}
	return hardware.Facts{GPUs: gpus, DMI: dmi, PlatformProfiles: profiles}, nil
}

func printFacts(out *ui.Progress, f hardware.Facts) {
	out.Printf("  %s %s (chassis %d)\n", f.DMI.Vendor, f.DMI.Product, f.DMI.ChassisType)
	if len(f.PlatformProfiles) > 0 {
		out.Printf("  platform profiles: %s\n", strings.Join(f.PlatformProfiles, " "))
	}
	for _, g := range f.GPUs {
		out.Printf("  %s %s: %s\n", g.Vendor, g.Class, g.BusID)
	}
}
