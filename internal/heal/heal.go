// Package heal is the self-healing orchestrator run before every rebuild.
// It sequences internal/gitignore, internal/framework, internal/imports,
// internal/generate, internal/links, internal/units, internal/refs,
// internal/computer, internal/staging and internal/userfile in the fixed
// order the bash self-heal.sh used, printing progress to the given
// io.Writer. It has no parsing or decision logic of its own
// (implementation-plan.md G10/G11): every package it calls takes explicit
// directories and returns values plus error; this file only sequences and
// reports.
package heal

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/computer"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/generate"
	"github.com/DeprecatedLuar/luxos/internal/gitignore"
	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/links"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/refs"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/units"
	"github.com/DeprecatedLuar/luxos/internal/userfile"
)

// gitignoreLines are the lines CONFIG_DIR's root .gitignore must contain;
// gitignore.Ensure appends whichever are missing and never removes or
// reorders anything else in the file.
var gitignoreLines = []string{"/modules/default.nix", "/modules/system", "/modules/local", "/local"}

// Nix invocations, replaceable so tests need neither network nor a
// working flake-file.
var (
	writeFlake = nix.WriteFlake
	flakeLock  = nix.FlakeLock
)

// selectionFile is a host's selection file, holding the "imports = [...]"
// block imports.Heal/List read and write (L4).
const selectionFile = "modules.nix"

const (
	// environmentFile is the global environment file at the config root;
	// environmentTemplate is the embedded template written when it is missing.
	environmentFile     = "environment"
	environmentTemplate = "templates/environment"
)

// Run performs the full self-heal sequence for host, in the order the bash
// self-heal.sh used: ensure the .gitignore, ensure the global environment file (created from
// the embedded template when missing), sync framework modules,
// validate and protect the host folder (L1-L3), heal host entrypoints,
// validate module boundaries, ensure /etc/nixos/boot.nix, materialize
// staging, generate+install flake-file.nix, detect GPUs and install
// framework/gpu.nix, then flake.nix via flake-file, and configuration.nix.
// exe is the absolute path to the running
// luxos binary, used by generate.Configuration for the shadow scripts.
// prune, when true, removes unresolvable import lines from the active
// host's entrypoint instead of erroring.
func Run(w io.Writer, p paths.Paths, host, exe string, prune bool) error {
	hostDir := filepath.Join(p.Local, host)

	// 1. gitignore
	fmt.Fprintln(w, "Ensuring .gitignore...")
	added, err := gitignore.Ensure(filepath.Join(p.Config, ".gitignore"), gitignoreLines)
	if err != nil {
		return err
	}
	for _, line := range added {
		fmt.Fprintf(w, "  added: %s\n", line)
	}

	// 1b. environment file
	fmt.Fprintln(w, "Ensuring environment file...")
	tmpl, err := framework.File(environmentTemplate)
	if err != nil {
		return err
	}
	envPath := filepath.Join(p.Config, environmentFile)
	created, err := userfile.Create(envPath, tmpl)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(w, "  created: %s\n", envPath)
	}

	// 2. framework modules sync
	fmt.Fprintln(w, "Syncing framework modules...")
	changes, err := framework.Sync(filepath.Join(p.Modules, "system"))
	if err != nil {
		return err
	}
	for _, c := range changes {
		line := fmt.Sprintf("%s: %s", c.Action, c.Path)
		if c.Action == framework.ActionCreated {
			fmt.Fprintln(w, line)
		} else {
			fmt.Fprintln(w, "Warning: "+line)
		}
	}

	// 3. validate and protect the host folder
	fmt.Fprintf(w, "Validating %s...\n", hostDir)
	if err := config.ValidateMachine(hostDir); err != nil {
		return err
	}
	protected, err := config.ProtectMachine(hostDir)
	if err != nil {
		return err
	}
	if protected {
		fmt.Fprintf(w, "  protected: %s\n", filepath.Join(hostDir, ".plsdonttouch.nix"))
	}

	// 4. ensure modules mirror
	fmt.Fprintf(w, "Ensuring %s's modules mirror...\n", host)
	if err := links.EnsureMirror(p.Local, p.Modules, host); err != nil {
		return err
	}

	// 4b. ensure modules/local -> .local/<host>/modules link
	fmt.Fprintf(w, "Ensuring modules/local -> .local/%s/modules link...\n", host)
	if err := links.EnsureLocalModules(p.Local, p.Modules, host); err != nil {
		return err
	}

	// 5. heal imports
	fmt.Fprintln(w, "Healing modules imports...")
	healChanges, healWarnings, err := imports.Heal(p.Local, p.Modules, host, prune)
	for _, c := range healChanges {
		fmt.Fprintln(w, formatImportChange(c))
	}
	for _, wm := range healWarnings {
		fmt.Fprintln(w, "Warning: "+wm)
	}
	if err != nil {
		return err
	}

	// 6. validate module boundaries
	fmt.Fprintln(w, "Validating module boundaries...")
	us, err := units.Walk(p.Modules, filepath.Join(hostDir, "modules"))
	if err != nil {
		return err
	}
	selection, err := imports.List(filepath.Join(hostDir, selectionFile))
	if err != nil {
		return err
	}
	violations, err := refs.Validate(p.Modules, us, selection)
	if err != nil {
		return err
	}
	if len(violations) > 0 {
		for _, v := range violations {
			fmt.Fprintf(w, "Error: %s: %s\n", v.File, v.Message)
		}
		return fmt.Errorf("module boundary violations: %d", len(violations))
	}

	// 7. ensure local link
	fmt.Fprintf(w, "Ensuring local -> .local/%s link...\n", host)
	if err := links.EnsureLocalLink(p.Config, p.Local, host); err != nil {
		return err
	}

	// 7a. ensure hardware-configuration.nix
	fmt.Fprintf(w, "Ensuring %s...\n", p.HardwareConfig)
	hwCreated, err := computer.EnsureHardwareConfig(p.HardwareConfig)
	if err != nil {
		return err
	}
	if hwCreated {
		fmt.Fprintf(w, "  created: %s (generated by nixos-generate-config; luxos never rewrites it)\n", p.HardwareConfig)
	}

	// 7b. ensure boot.nix
	fmt.Fprintf(w, "Ensuring %s...\n", p.BootConfig)
	bootCreated, err := computer.EnsureBoot(p.BootConfig, p.Sys, p.Mounts)
	if err != nil {
		return err
	}
	if bootCreated {
		fmt.Fprintf(w, "  created: %s (review it; luxos never rewrites it)\n", p.BootConfig)
	}

	// 8. materialize staging
	fmt.Fprintf(w, "Materializing %s for %s...\n", p.Staging, host)
	if err := staging.Materialize(p.Staging, p.Modules, hostDir, p.HardwareConfig, p.BootConfig, filepath.Join(hostDir, "flake.lock"), envPath); err != nil {
		return err
	}

	// 9. generate flake-file.nix, let flake-file write flake.nix, lock
	fmt.Fprintln(w, "Generating flake.nix with flake-file...")
	bootstrap, err := generate.FlakeBootstrap(host)
	if err != nil {
		return err
	}
	if err := staging.Install(p.Staging, "flake-file.nix", bootstrap); err != nil {
		return err
	}

	// 9b. detect GPUs and install the fact file
	fmt.Fprintln(w, "Detecting GPUs...")
	gpus, err := computer.DetectGPUs(p.Sys)
	if err != nil {
		return err
	}
	for _, g := range gpus {
		fmt.Fprintf(w, "  %s %s: %s\n", g.Vendor, g.Class, g.BusID)
	}
	gpuContent, err := computer.RenderGPUs(gpus)
	if err != nil {
		return err
	}
	if err := staging.Install(p.Staging, "framework/gpu.nix", gpuContent); err != nil {
		return err
	}

	if err := writeFlake(p.Staging); err != nil {
		return err
	}
	if err := flakeLock(p.Staging); err != nil {
		return err
	}
	hostLock := filepath.Join(hostDir, "flake.lock")
	changed, err := staging.LockChanged(p.Staging, hostLock)
	if err != nil {
		return err
	}
	if changed {
		if err := staging.CopyLockBack(p.Staging, hostLock); err != nil {
			return err
		}
		fmt.Fprintf(w, "  locked new inputs: %s\n", hostLock)
	}

	// 10. generate and install configuration.nix
	fmt.Fprintln(w, "Generating configuration.nix...")
	configContent, err := generate.Configuration(host, exe)
	if err != nil {
		return err
	}
	if err := staging.Install(p.Staging, "configuration.nix", configContent); err != nil {
		return err
	}

	// 11. heal /etc/nixos
	fmt.Fprintln(w, "Ensuring /etc/nixos...")
	actions, err := links.EnsureEtcNixos(p.EtcNixos)
	if err != nil {
		return err
	}
	for _, a := range actions {
		fmt.Fprintln(w, a)
	}

	return nil
}

// formatImportChange renders one imports.Change the way bash's
// imports::retarget printed it: "<file>: ./<old> -> ./<new>", or
// "<file>: removed ./<old>" when New is empty.
func formatImportChange(c imports.Change) string {
	if c.New == "" {
		return fmt.Sprintf("%s: removed ./%s", c.File, c.Old)
	}
	return fmt.Sprintf("%s: ./%s -> ./%s", c.File, c.Old, c.New)
}
