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
	flakeFileNix      = "flake-file.nix"
	hardwareFactsNix  = "hardware-facts.nix"
	configurationNix  = "configuration.nix"
	stagedEnvironment = "environment"

	// rootLocalEntry is the entry of the modules dir, and of config/modules,
	// that holds a host's own modules.
	rootLocalEntry  = "local"
	localModulesDir = "modules"

	stagedModulesDir   = "modules"
	entrypointFile     = "default.nix"
	stagedMachineNix   = "machine.nix"
	stagedPlsdonttouch = ".plsdonttouch.nix"

	lockFileName = "flake.lock"

	frameworkTemplateDir = "framework/"
	flakeTemplate        = "flake.nix.tmpl"
)

// Materialize regenerates every luxos-owned entry of stagingDir for h
// (pruning them first): the framework files, flake.nix, framework/flake-file.nix,
// framework/hardware-facts.nix, framework/configuration.nix, config/ and the host's
// flake.lock. Every symlink under h.ModulesDir and h.HostDir is dereferenced; a
// dangling one refuses the whole call, naming it, before anything is touched.
// Nothing is read through modules/local or modules/default.nix, so the stage
// does not depend on which host those links point at.
//
// config/modules holds the shared tree, h's own modules under local/ with
// hwDir as local/hardware-support, and h's selection as default.nix. Each
// module of h.Modules with Shadows set is staged at its original's location
// (same category, its own file or folder name), and every import of that name
// in the staged default.nix is pointed there. The original is not staged; no
// source file is rewritten.
//
// inputs are the flake-file.inputs declarations of the modules h builds plus
// the host's machine.nix; they become flake.nix's `inputs = { ... };` block
// beyond the flake-file pin.
//
// settings names the units whose settings files (config.SettingsPath) are
// copied to config/settings/ and imported by framework/configuration.nix.
func Materialize(stagingDir string, h *modules.Host, hwDir, environmentFile string, inputs []nix.InputDecl, facts hardware.Facts, settings []string) error {
	inputsNix := nix.RenderInputs(inputs)
	if inputsNix == "" {
		return errors.New("staging: flake inputs are required")
	}
	if err := checkNoDanglingLinks(h.ModulesDir); err != nil {
		return err
	}
	if err := checkNoDanglingLinks(h.HostDir); err != nil {
		return err
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
	if err := writeFlakeNix(stagingDir, inputsNix); err != nil {
		return err
	}

	generated := []struct {
		name   string
		render func() ([]byte, error)
	}{
		{flakeFileNix, func() ([]byte, error) { return flakeBootstrap(h.Name) }},
		{hardwareFactsNix, func() ([]byte, error) { return hardware.Render(facts) }},
		{configurationNix, func() ([]byte, error) { return configuration(h.Name, settings) }},
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

	if err := stageModules(h, hwDir, filepath.Join(cfgDir, stagedModulesDir)); err != nil {
		return err
	}
	for _, name := range []string{stagedMachineNix, stagedPlsdonttouch} {
		if err := copyFile(filepath.Join(h.HostDir, name), filepath.Join(cfgDir, name)); err != nil {
			return err
		}
	}

	for _, name := range settings {
		dst := filepath.Join(cfgDir, config.SettingsDir, filepath.FromSlash(name)+".nix")
		if err := copyFile(config.SettingsPath(h.HostDir, name), dst); err != nil {
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

// stageModules writes h's config/modules tree into dst.
func stageModules(h *modules.Host, hwDir, dst string) error {
	sharedSkip := map[string]bool{rootLocalEntry: true, entrypointFile: true}
	localSkip := map[string]bool{config.HardwareUnitName: true}
	var shadows []modules.Module
	for _, m := range h.Modules {
		if m.Shadows == "" {
			continue
		}
		shadows = append(shadows, m)
		sharedSkip[m.Shadows] = true
		localSkip[strings.TrimPrefix(m.Path, modules.LocalPrefix)] = true
	}
	if err := copyDerefSkip(h.ModulesDir, dst, sharedSkip); err != nil {
		return err
	}

	localDst := filepath.Join(dst, rootLocalEntry)
	hostModules := filepath.Join(h.HostDir, localModulesDir)
	_, err := os.Stat(hostModules)
	switch {
	case err == nil:
		err = copyDerefSkip(hostModules, localDst, localSkip)
	case os.IsNotExist(err):
		err = os.MkdirAll(localDst, dirMode)
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(hwDir); err != nil {
		return fmt.Errorf("hardware folder %s: %w\n  run: luxos rebuild", hwDir, err)
	}
	if err := copyDeref(hwDir, filepath.Join(localDst, config.HardwareUnitName)); err != nil {
		return err
	}

	entry := filepath.Join(dst, entrypointFile)
	if err := copyFile(filepath.Join(h.HostDir, modules.SelectionFile), entry); err != nil {
		return err
	}

	for _, m := range shadows {
		staged := m.StagedPath()
		info, err := os.Stat(m.Abs)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, staged)
		if info.IsDir() {
			err = copyDeref(m.Abs, target)
		} else {
			err = copyFile(m.Abs, target)
		}
		if err != nil {
			return err
		}

		name := m.Name
		_, ok, err := nix.RetargetImports(entry, func(p string) bool { return modules.NameFromPath(p) == name }, staged)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("cannot retarget imports of shadow '%s': %s has no recognizable imports block", name, entry)
		}
	}
	return nil
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

func writeFlakeNix(dst, inputsNix string) error {
	out, err := templates.Render(flakeTemplate, flakeNixData{Inputs: inputsNix})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, flakeNix), out, fileMode)
}

type flakeNixData struct {
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

// SavePrevious copies every existing owned entry of stagingDir into prevDir.
// An existing prevDir is left alone.
func SavePrevious(stagingDir, prevDir string) error {
	if _, err := os.Lstat(prevDir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(prevDir, dirMode); err != nil {
		return err
	}
	for _, name := range owned {
		src := filepath.Join(stagingDir, name)
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := copyDeref(src, filepath.Join(prevDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func RestorePrevious(stagingDir, prevDir string) error {
	if _, err := os.Lstat(prevDir); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := Replace(prevDir, stagingDir); err != nil {
		return err
	}
	if err := Seal(stagingDir); err != nil {
		return err
	}
	return os.RemoveAll(prevDir)
}

func DropPrevious(prevDir string) error {
	return os.RemoveAll(prevDir)
}

// Baseline returns the tree running modules are compared against: the saved
// previous stage's modules when it exists, else the staging directory's, else
// "" when neither holds one.
func Baseline(stagingDir, prevDir string) (string, error) {
	base := filepath.Join(stagingDir, configDir, stagedModulesDir)
	if _, err := os.Stat(prevDir); err == nil {
		base = filepath.Join(prevDir, configDir, stagedModulesDir)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if _, err := os.Stat(base); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return base, nil
}
