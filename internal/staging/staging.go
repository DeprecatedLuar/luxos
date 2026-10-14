// Package staging materializes the active host's flake root: a real-file
// copy of the shared modules tree and the active host's own tree (symlinks
// dereferenced), plus the whitelisted slice of the framework a flake needs
// to evaluate itself. Every directory is a parameter; nothing here calls
// sudo (it runs as root already).
package staging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/hardware"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

// frameworkFiles are copied verbatim into framework/.
var frameworkFiles = []string{
	"system.nix", "units.nix", "overlay.nix", "outputs.nix", "environment.nix",
	"luxos-hardware.nix", "nvidia-generations.nix", "luxos-hardware-defaults.nix",
}

const (
	dirMode        = 0755
	fileMode       = 0644
	sealedFileMode = 0444

	frameworkDir = "framework"
	configDir    = "config"

	flakeNix          = "flake.nix"
	hardwareFactsNix  = "hardware-facts.nix"
	configurationNix  = "configuration.nix"
	stagedEnvironment = "environment"

	stagedModulesDir     = "modules"
	selectionTemplate    = "modules.nix.tmpl"
	entryTemplate        = "entry/default.nix"
	stagedHardwareConfig = config.HardwareConfigFile
	stagedHostNix        = "host.nix"
	stagedPlsdonttouch   = ".plsdonttouch.nix"

	lockFileName = "flake.lock"

	frameworkTemplateDir = "framework/"
	flakeTemplate        = "flake.nix.tmpl"

	luxosInputName   = "luxos"
	luxosInputURL    = "github:DeprecatedLuar/luxos/main"
	luxosInputSource = "luxos built-in"
)

// Materialize regenerates every luxos-owned entry of stagingDir for h
// (pruning them first): the framework files, flake.nix,
// framework/hardware-facts.nix, framework/configuration.nix, config/ and the host's
// flake.lock. Every symlink under h.ModulesDir, h.HostDir and h.MachineDir is
// dereferenced; a dangling one refuses the whole call, naming it, before
// anything is touched. Nothing is read through modules/host, modules/machine
// or modules/default.nix, so the stage does not depend on which folders those
// links point at.
//
// config/modules holds the shared tree, h's units under host/, the machine's
// under machine/, the fixed default.nix entry and both selections rendered as
// host/default.nix and machine/default.nix. A unit with Shadows set is staged
// at the location of the unit it hides, which is not staged, and every
// selection line naming it points there; no source file is rewritten. The
// machine's hardware-configuration.nix is staged as config/hardware-configuration.nix.
//
// inputs are the luxos.inputs declarations of the modules h builds plus
// the host's host.nix; with the built-in luxos input they are merged
// (nix.MergeInputs) into flake.nix's `inputs = { ... };` block.
//
// settings are the units whose settings files are copied to
// config/settings/ and imported by framework/configuration.nix.
func Materialize(stagingDir string, h *modules.Host, environmentFile string, inputs []nix.InputDecl, facts hardware.Facts, settings []modules.StagedSetting) error {
	if len(inputs) == 0 {
		return errors.New("staging: flake inputs are required")
	}
	merged, err := nix.MergeInputs(append(slices.Clone(inputs), luxosInput()))
	if err != nil {
		return err
	}
	inputsNix := nix.RenderInputs(merged)
	if h.MachineDir == "" {
		return errors.New("staging: the machine folder is required")
	}
	for _, dir := range []string{h.ModulesDir, h.HostDir, h.MachineDir} {
		if err := checkNoDanglingLinks(dir); err != nil {
			return err
		}
	}

	if err := Prune(stagingDir); err != nil {
		return err
	}

	fwDir := filepath.Join(stagingDir, frameworkDir)
	cfgDir := filepath.Join(stagingDir, configDir)
	if err := os.MkdirAll(fwDir, dirMode); err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, dirMode); err != nil {
		return err
	}

	for _, name := range frameworkFiles {
		if err := writeFrameworkFile(fwDir, name); err != nil {
			return err
		}
	}
	if err := writeFlakeNix(stagingDir, h.Name, inputsNix); err != nil {
		return err
	}

	generated := []struct {
		name   string
		render func() ([]byte, error)
	}{
		{hardwareFactsNix, func() ([]byte, error) { return hardware.Render(facts) }},
		{configurationNix, func() ([]byte, error) { return configuration(h.Name, settingNames(settings)) }},
	}
	for _, g := range generated {
		content, err := g.render()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(fwDir, g.name), content, fileMode); err != nil {
			return err
		}
	}

	if err := stageModules(h, filepath.Join(cfgDir, stagedModulesDir)); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(h.MachineDir, config.HardwareConfigFile), filepath.Join(cfgDir, stagedHardwareConfig)); err != nil {
		return err
	}
	for _, name := range []string{stagedHostNix, stagedPlsdonttouch} {
		if err := copyFile(filepath.Join(h.HostDir, name), filepath.Join(cfgDir, name)); err != nil {
			return err
		}
	}

	for _, st := range settings {
		dst := filepath.Join(cfgDir, config.SettingsDir, filepath.FromSlash(st.Name)+".nix")
		if err := copyFile(st.File, dst); err != nil {
			return err
		}
	}

	if err := copyFile(environmentFile, filepath.Join(cfgDir, stagedEnvironment)); err != nil {
		return err
	}

	lockFile := filepath.Join(h.HostDir, lockFileName)
	if _, err := os.Stat(lockFile); err == nil {
		if err := copyFile(lockFile, filepath.Join(stagingDir, lockFileName)); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// stageModules writes config/modules: the shared tree, the host's units
// under host/, the machine's under machine/, the fixed entry and both
// selections, rendered. A shadow is staged at the location of the unit it
// hides, which is not staged, and every selection line naming it points there.
func stageModules(h *modules.Host, dst string) error {
	skip := map[string]map[string]bool{
		config.ScopeShared:  {config.DefaultFile: true, config.HostLinkName: true, config.MachineLinkName: true},
		config.ScopeHost:    {config.DefaultFile: true},
		config.ScopeMachine: {config.DefaultFile: true},
	}
	var shadows []modules.Module
	for _, m := range h.Hidden {
		skip[m.Scope][scopeRel(m)] = true
	}
	for _, m := range h.Modules {
		if m.Shadows != "" {
			shadows = append(shadows, m)
			skip[m.Scope][scopeRel(m)] = true
		}
	}

	if err := copyDerefSkip(h.ModulesDir, dst, skip[config.ScopeShared]); err != nil {
		return err
	}
	for _, s := range []struct{ scope, dir string }{{config.ScopeHost, h.HostDir}, {config.ScopeMachine, h.MachineDir}} {
		src := filepath.Join(s.dir, config.ScopeModulesDir)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Join(dst, s.scope), dirMode); err != nil {
				return err
			}
			continue
		}
		if err := copyDerefSkip(src, filepath.Join(dst, s.scope), skip[s.scope]); err != nil {
			return err
		}
	}
	for _, m := range shadows {
		if err := copyUnit(m.Abs, filepath.Join(dst, m.StagedPath())); err != nil {
			return err
		}
	}

	entry, err := templates.File(entryTemplate)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dst, config.DefaultFile), entry, fileMode); err != nil {
		return err
	}
	for _, sel := range h.Selections {
		lines := make([]string, 0, len(sel.Paths))
		for _, p := range sel.Paths {
			lines = append(lines, config.LineOf(sel.Scope, stagedSelectionPath(h, p)))
		}
		rendered, err := templates.Render(selectionTemplate, lines)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, sel.Scope, config.DefaultFile), rendered, fileMode); err != nil {
			return err
		}
	}
	return nil
}

// scopeRel is m's path inside its own scope's folder.
func scopeRel(m modules.Module) string {
	return strings.TrimPrefix(m.Path, m.Scope+"/")
}

func copyUnit(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDeref(src, dst)
	}
	return copyFile(src, dst)
}

func settingNames(settings []modules.StagedSetting) []string {
	names := make([]string, len(settings))
	for i, st := range settings {
		names[i] = st.Name
	}
	return names
}

// stagedSelectionPath is where a selection line points in the stage: a
// shadowed name points at its shadow's staged location.
func stagedSelectionPath(h *modules.Host, path string) string {
	if m, ok := h.Find(modules.NameFromPath(path)); ok && m.Shadows != "" {
		return m.StagedPath()
	}
	return path
}

func checkNoDanglingLinks(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("dangling symlink, cannot stage: %s", path)
			}
			return err
		}
		return nil
	})
}

func writeFlakeNix(dst, host, inputsNix string) error {
	if host == "" {
		return errors.New("staging.writeFlakeNix: host name is required")
	}
	out, err := templates.Render(flakeTemplate, flakeNixData{Host: host, Inputs: inputsNix})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, flakeNix), out, fileMode)
}

type flakeNixData struct {
	Host   string
	Inputs string
}

func writeFrameworkFile(dst, name string) error {
	data, err := templates.File(frameworkTemplateDir + name)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, name), data, fileMode)
}

// copyDeref copies src into dst, dereferencing every symlink. No symlink survives under dst.
func copyDeref(src, dst string) error {
	return copyDerefSkip(src, dst, nil)
}

// copyDerefSkip is copyDeref that leaves out every entry whose path relative
// to src is a key of skip. Path is through any followed symlink.
func copyDerefSkip(src, dst string, skip map[string]bool) error {
	return copyDerefSkipAt(src, dst, "", skip)
}

func copyDerefSkipAt(src, dst, prefix string, skip map[string]bool) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if key := filepath.Join(prefix, rel); rel != "." && skip[key] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return copyDerefSkipAt(resolved, target, filepath.Join(prefix, rel), skip)
			}
			return copyFile(resolved, target)
		}

		if d.IsDir() {
			return os.MkdirAll(target, dirMode)
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return err
	}
	return os.WriteFile(dst, data, fileMode)
}

// Seal makes every file luxos generated read-only, so an accidental edit
// under /etc/nixos fails visibly instead of vanishing on the next rebuild.
// Directories stay 0755: unlinking is governed by the parent's mode, so
// Prune still works. This does not stop root, which is the common case
// under sudo - it is a signal, not a lock.
func Seal(stagingDir string) error {
	for _, name := range owned {
		root := filepath.Join(stagingDir, name)
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			switch {
			case d.IsDir():
				return os.Chmod(p, dirMode)
			case d.Type().IsRegular():
				return os.Chmod(p, sealedFileMode)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Baseline returns the modules tree of the stage in stagingDir, the last one
// that built and activated, or "" when it holds none.
func Baseline(stagingDir string) (string, error) {
	base := filepath.Join(stagingDir, configDir, stagedModulesDir)
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return base, nil
}

// luxosInput is the declaration of the luxos binary's own flake input,
// installed by framework/system.nix.
func luxosInput() nix.InputDecl {
	return nix.InputDecl{
		File: luxosInputSource,
		Name: luxosInputName,
		URL:  luxosInputURL,
		Value: map[string]any{
			"url":    luxosInputURL,
			"inputs": map[string]any{"nixpkgs": map[string]any{"follows": "nixpkgs"}},
		},
	}
}
