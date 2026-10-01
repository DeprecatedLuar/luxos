// Package staging materializes the active host's flake root: a real-file
// copy of the shared modules tree and the active host's own tree (symlinks
// dereferenced), plus the whitelisted slice of the framework a flake needs
// to evaluate itself. Every directory is a parameter; nothing here calls
// sudo (it runs as root already).
package staging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	dirMode        = 0755
	fileMode       = 0644
	sealedFileMode = 0444

	frameworkDir = "framework"
	configDir    = "config"

	flakeNix   = "flake.nix"
	systemNix  = "system.nix"
	unitsNix   = "units.nix"
	overlayNix = "overlay.nix"
	outputsNix = "outputs.nix"

	environmentNix        = "environment.nix"
	luxosHardwareNix      = "luxos-hardware.nix"
	luxosHardwareDefaults = "luxos-hardware-defaults.nix"
	nvidiaGenerations     = "nvidia-generations.nix"
	stagedEnvironment     = "environment"

	stagedModulesDir   = "modules"
	entrypointFile     = "default.nix"
	stagedMachineNix   = "machine.nix"
	stagedPlsdonttouch = ".plsdonttouch.nix"

	lockFileName = "flake.lock"

	// bundleModulesDir is the folder of a bundle's submodules.
	bundleModulesDir = "modules"
)

// Materialize regenerates the luxos-owned entries of stagingDir (pruning
// them first). lockFile is the active host's own flake.lock
// (hostDir/flake.lock), not a config-root one; entries outside the owned
// list are left alone for Adopt to handle. Every symlink under modulesDir
// and hostDir is dereferenced; a dangling one refuses the whole call,
// naming it, before anything is touched.
//
// us is the host's unit set: each unit with Shadows set is staged at its
// original's location (same category, its own file or folder name), and
// every import of that name in the staged config/modules/default.nix is
// pointed there. The original is not staged; no source file is rewritten.
//
// inputsNix is the body of flake.nix's `inputs = { ... };` block beyond the
// flake-file pin (nix.RenderInputs output); Materialize only writes it in.
func Materialize(stagingDir, modulesDir, hostDir string, us []modules.Module, lockFile, environmentFile, inputsNix string) error {
	if err := checkNoDanglingLinks(modulesDir); err != nil {
		return err
	}
	if err := checkNoDanglingLinks(hostDir); err != nil {
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

	if err := writeFrameworkFile(fwDir, systemNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, unitsNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, overlayNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, outputsNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, environmentNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, luxosHardwareNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, nvidiaGenerations); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, luxosHardwareDefaults); err != nil {
		return err
	}
	if err := writeFlakeNix(stagingDir, inputsNix); err != nil {
		return err
	}

	if err := stageModules(modulesDir, filepath.Join(cfgDir, stagedModulesDir), us); err != nil {
		return err
	}
	for _, name := range []string{stagedMachineNix, stagedPlsdonttouch} {
		if err := copyFile(filepath.Join(hostDir, name), filepath.Join(cfgDir, name)); err != nil {
			return err
		}
	}

	if err := copyFile(environmentFile, filepath.Join(cfgDir, stagedEnvironment)); err != nil {
		return err
	}

	if _, err := os.Stat(lockFile); err == nil {
		if err := copyFile(lockFile, filepath.Join(stagingDir, lockFileName)); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}

func StagedPath(u modules.Module) string {
	if u.Shadows == "" {
		return u.Path
	}
	return filepath.Join(filepath.Dir(u.Shadows), filepath.Base(u.Path))
}

// UnitChanged reports whether u's files under modulesDir (symlinks
// dereferenced, as Materialize copies them) differ from its copy under
// stagedModulesDir. A unit missing on the staged side is changed. A bundle's
// modules/ folder is not part of it: a submodule's change marks the submodule.
func UnitChanged(modulesDir, stagedModulesDir string, u modules.Module) (bool, error) {
	skip := ""
	if modules.IsBundle(filepath.Join(modulesDir, u.Path)) {
		skip = bundleModulesDir
	}
	src, err := readUnitFiles(filepath.Join(modulesDir, u.Path), skip)
	if err != nil {
		return false, err
	}
	dst, err := readUnitFiles(filepath.Join(stagedModulesDir, StagedPath(u)), skip)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if len(src) != len(dst) {
		return true, nil
	}
	for rel, data := range src {
		other, ok := dst[rel]
		if !ok || !bytes.Equal(data, other) {
			return true, nil
		}
	}
	return false, nil
}

// readUnitFiles returns the contents of root keyed by path relative to root;
// a single file is keyed ".". Symlinks are followed. skipTop, when non-empty,
// names a top-level entry of root left out.
func readUnitFiles(root, skipTop string) (map[string][]byte, error) {
	files := map[string][]byte{}
	var walk func(path, rel string) error
	walk = func(path, rel string) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[rel] = data
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if path == root && skipTop != "" && e.Name() == skipTop {
				continue
			}
			if err := walk(filepath.Join(path, e.Name()), filepath.Join(rel, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, "."); err != nil {
		return nil, err
	}
	return files, nil
}

func stageModules(modulesDir, dst string, us []modules.Module) error {
	skip := make(map[string]bool)
	var shadows []modules.Module
	for _, u := range us {
		if u.Shadows == "" {
			continue
		}
		shadows = append(shadows, u)
		skip[u.Shadows] = true
		skip[u.Path] = true
	}
	if err := copyDerefSkip(modulesDir, dst, skip); err != nil {
		return err
	}

	for _, u := range shadows {
		staged := StagedPath(u)
		src := filepath.Join(modulesDir, u.Path)
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, staged)
		if info.IsDir() {
			err = copyDeref(src, target)
		} else {
			err = copyFile(src, target)
		}
		if err != nil {
			return err
		}

		entry := filepath.Join(dst, entrypointFile)
		name := u.Name
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

func Install(stagingDir, rel string, content []byte) error {
	dest := filepath.Join(stagingDir, rel)
	if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
		return err
	}
	if err := os.WriteFile(dest, content, fileMode); err != nil {
		return err
	}
	return os.Chmod(dest, fileMode)
}

type LockInput struct {
	Type  string
	Owner string
	Repo  string
	Rev   string
}

// ReadLockInput returns the locked identity of the node called name in
// lockFile. A missing file or an absent node reports false with no error;
// malformed JSON is an error.
func ReadLockInput(lockFile, name string) (LockInput, bool, error) {
	data, err := os.ReadFile(lockFile)
	if err != nil {
		if os.IsNotExist(err) {
			return LockInput{}, false, nil
		}
		return LockInput{}, false, err
	}
	var lock struct {
		Nodes map[string]struct{ Locked LockInput }
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return LockInput{}, false, fmt.Errorf("parse %s: %w", lockFile, err)
	}
	node, ok := lock.Nodes[name]
	if !ok {
		return LockInput{}, false, nil
	}
	return node.Locked, true, nil
}

const LockRootNode = "root"

type LockRef struct {
	Type  string `json:"type"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Ref   string `json:"ref"`
	Rev   string `json:"rev"`
	URL   string `json:"url"`
}

// LockNode is one node of a flake.lock. Inputs maps an input name to the
// node key it points at; `follows` entries are skipped.
type LockNode struct {
	Inputs   map[string]string
	Original LockRef
	Locked   LockRef
}

type LockGraph struct {
	Root  []string
	Nodes map[string]LockNode
}

// ReadLockGraph reads lockFile into a LockGraph. A missing file returns an
// empty graph and no error.
func ReadLockGraph(lockFile string) (LockGraph, error) {
	graph := LockGraph{Nodes: map[string]LockNode{}}

	data, err := os.ReadFile(lockFile)
	if err != nil {
		if os.IsNotExist(err) {
			return graph, nil
		}
		return LockGraph{}, err
	}

	var lock struct {
		Nodes map[string]struct {
			Inputs   map[string]json.RawMessage `json:"inputs"`
			Original LockRef                    `json:"original"`
			Locked   LockRef                    `json:"locked"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return LockGraph{}, fmt.Errorf("parse %s: %w", lockFile, err)
	}

	for key, raw := range lock.Nodes {
		node := LockNode{Inputs: map[string]string{}, Original: raw.Original, Locked: raw.Locked}
		for name, target := range raw.Inputs {
			var nodeKey string
			if err := json.Unmarshal(target, &nodeKey); err != nil {
				continue // a follows array
			}
			node.Inputs[name] = nodeKey
		}
		graph.Nodes[key] = node
	}

	for name := range graph.Nodes[LockRootNode].Inputs {
		graph.Root = append(graph.Root, name)
	}
	sort.Strings(graph.Root)
	return graph, nil
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
	if inputsNix == "" {
		return errors.New("staging: flake inputs are required")
	}
	out, err := templates.Render("flake.nix.tmpl", flakeNixData{Inputs: inputsNix})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, flakeNix), out, fileMode)
}

type flakeNixData struct {
	Inputs string
}

func writeFrameworkFile(dst, name string) error {
	data, err := templates.File("framework/" + name)
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
	if err := Prune(stagingDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(prevDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyDeref(filepath.Join(prevDir, e.Name()), filepath.Join(stagingDir, e.Name())); err != nil {
			return err
		}
	}
	if err := Seal(stagingDir); err != nil {
		return err
	}
	return os.RemoveAll(prevDir)
}

func DropPrevious(prevDir string) error {
	return os.RemoveAll(prevDir)
}
