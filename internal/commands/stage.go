package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
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
// no network.
type flakeSteps struct {
	lock func(stagingDir string) error // locks missing inputs
}

var realFlakeSteps = flakeSteps{lock: nix.FlakeLock}

// configDirs are the folders of p that config.Ensure works on.
func configDirs(p paths.Paths) config.Dirs {
	return config.Dirs{Config: p.Config, Modules: p.Modules, Hosts: p.Hosts, Machines: p.Machines, Sys: p.Sys, Dev: p.Dev, Mounts: p.Mounts}
}

// prepare brings CONFIG_DIR, host's selections, its users, its settings and
// its passwords to the state a stage of host needs, and returns the host to
// stage. syncUsers may create a user's selection and starter module, so the
// host is loaded again after it.
func prepare(out *ui.Progress, p paths.Paths, host string, prune bool) (*modules.Host, error) {
	machineDir, err := config.Ensure(out, configDirs(p), host)
	if err != nil {
		return nil, err
	}
	h, err := healAndValidate(out, p, host, machineDir, prune)
	if err != nil {
		return nil, err
	}
	users, err := builtUsers(h)
	if err != nil {
		return nil, err
	}
	if err := syncUsers(out, users); err != nil {
		return nil, err
	}
	if h, err = modules.Load(p.Modules, h.HostDir, machineDir); err != nil {
		return nil, err
	}
	if err := syncSettings(out, h); err != nil {
		return nil, err
	}
	if err := checkPasswords(out, users, p.Keys, keyOwnerUID); err != nil {
		return nil, err
	}
	return h, nil
}

// healAndValidate checks both selections and every selected user's hold only
// imports, moves cross-scope lines, heals broken lines, then checks the active host's
// module boundaries, returning the host it validated. prune removes
// unresolvable lines from the active host's and this computer's selections
// instead of failing.
func healAndValidate(out *ui.Progress, p paths.Paths, host, machineDir string, prune bool) (*modules.Host, error) {
	hostDir := filepath.Join(p.Hosts, host)
	out.Printf("Checking selections...\n")
	for _, dir := range []string{hostDir, machineDir} {
		if err := modules.CheckSelection(config.SelectionPath(dir)); err != nil {
			return nil, err
		}
	}

	out.Printf("Healing modules imports...\n")
	moves, changes, warnings, err := modules.Heal(p.Modules, p.Hosts, p.Machines, host, machineDir, prune)
	for _, m := range moves {
		out.Warnf("moved: %s → %s", m.Name, displaySelection(p, m.To))
	}
	for _, c := range changes {
		out.Changef("%s", formatImportChange(c))
	}
	for _, wm := range warnings {
		out.Warnf("%s", wm)
	}
	if err != nil {
		return nil, err
	}

	out.Printf("Validating module boundaries...\n")
	h, err := modules.Load(p.Modules, hostDir, machineDir)
	if err != nil {
		return nil, err
	}
	for _, sel := range h.SelectedUsers() {
		if err := modules.CheckSelection(sel.File); err != nil {
			return nil, err
		}
	}
	violations, err := h.Validate()
	if err != nil {
		return nil, err
	}
	if len(violations) > 0 {
		for _, v := range violations {
			out.Errf("%s: %s", v.File, v.Message)
		}
		return nil, fmt.Errorf("module boundary violations: %d", len(violations))
	}
	return h, out.Err()
}

// displaySelection names a selection file by its path under modules/.
func displaySelection(p paths.Paths, file string) string {
	if strings.HasPrefix(file, p.Machines+string(filepath.Separator)) {
		return "modules/machine/default.nix"
	}
	return "modules/host/default.nix"
}

// syncSettings brings h's settings files in line with its selected units'
// options.nix, failing on any unit that breaks a settings rule.
func syncSettings(out *ui.Progress, h *modules.Host) error {
	out.Printf("Syncing module settings...\n")
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

// checkAssertions fails when the staged host's configuration has failed
// assertions, so the build never starts on a configuration NixOS rejects.
func checkAssertions(out *ui.Progress, stageDir, host string) error {
	out.Printf("Checking assertions...\n")
	failed, err := nix.FailedAssertions(stageDir, host)
	if err != nil {
		return err
	}
	for _, message := range failed {
		out.Errf("%s", strings.TrimSpace(message))
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d failed assertion(s)", len(failed))
	}
	return nil
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
		return fmt.Sprintf("%s: removed %s", c.File, c.Old)
	}
	return fmt.Sprintf("%s: %s -> %s", c.File, c.Old, c.New)
}

// stage writes h's stage into dir from CONFIG_DIR, which it only reads
// (apart from the host's flake.lock, when staging locked something new).
// It touches nothing in dir but the entries luxos owns.
func stage(out *ui.Progress, p paths.Paths, h *modules.Host, dir string, steps flakeSteps) error {
	files, err := h.InputFiles()
	if err != nil {
		return err
	}
	declFiles := make([]string, len(files))
	for i, f := range files {
		declFiles[i] = f.File
	}
	inputs, err := nix.InputDecls(declFiles...)
	if err != nil {
		return err
	}

	out.Printf("Detecting hardware...\n")
	facts, err := detectFacts(p.Sys)
	if err != nil {
		return err
	}
	printFacts(out, facts)

	out.Printf("Materializing %s for %s...\n", dir, h.Name)
	if err := staging.Materialize(dir, h, filepath.Join(p.Config, config.EnvironmentFile), inputs, facts, h.StagedSettings()); err != nil {
		return err
	}

	out.Printf("Locking flake inputs...\n")
	if err := steps.lock(dir); err != nil {
		return err
	}
	hostLock := filepath.Join(h.HostDir, flakeLockName)
	changed, err := config.CopyLockBack(filepath.Join(dir, flakeLockName), hostLock)
	if err != nil {
		return err
	}
	if changed {
		out.Changef("  locked new inputs: %s", hostLock)
	}
	return out.Err()
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
	cpuVendor, err := hardware.DetectCPUVendor(sysDir)
	if err != nil {
		return hardware.Facts{}, err
	}
	return hardware.Facts{GPUs: gpus, DMI: dmi, CPUVendor: cpuVendor, PlatformProfiles: profiles}, nil
}

func printFacts(out *ui.Progress, f hardware.Facts) {
	out.Printf("  %s %s (chassis %d, cpu %s)\n", f.DMI.Vendor, f.DMI.Product, f.DMI.ChassisType, f.CPUVendor)
	if len(f.PlatformProfiles) > 0 {
		out.Printf("  platform profiles: %s\n", strings.Join(f.PlatformProfiles, " "))
	}
	for _, g := range f.GPUs {
		out.Printf("  %s %s: %s\n", g.Vendor, g.Class, g.BusID)
	}
}

// withTempStage stages host into a new temporary directory named by pattern,
// calls fn with it and removes it. Staging progress is rebuild's output;
// failures come back as errors.
func withTempStage(p paths.Paths, host, pattern string, fn func(dir string) error) (err error) {
	tmp, err := os.MkdirTemp("", pattern)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()

	machineDir, err := config.LinkedMachineDir(p.Modules)
	if err != nil {
		return err
	}
	h, err := modules.Load(p.Modules, filepath.Join(p.Hosts, host), machineDir)
	if err != nil {
		return err
	}
	if err := stage(ui.NewProgress(io.Discard), p, h, tmp, realFlakeSteps); err != nil {
		return err
	}
	return fn(tmp)
}
