package modules

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// State is where a module stands between the selection, the running
// generation and the staged tree.
type State string

const (
	Active   State = "active"   // selected and running
	Staged   State = "staged"   // selected, not running yet
	Leftover State = "leftover" // running, no longer selected
	Pulled   State = "pulled"   // not selected, referenced by one that is
	Off      State = "off"
	Modified State = "modified" // selected and running, files differ from the baseline
	Removed  State = "removed"  // running, but names no module in the config
)

// Status is one module's state.
type Status struct {
	Module   Module
	State    State
	PulledBy []string // set when State is Pulled
	Inputs   []string // flake inputs the module declares
}

// StatusOf reports every module of h plus every running import that names no
// module (State Removed, Module.Path from the running import). running is the
// running generation's selection (nil when unknown); baseline is the tree to
// compare running modules against ("" for none).
func StatusOf(h *Host, running []string, baseline string) ([]Status, error) {
	enabled := make(map[string]bool, len(h.Selection))
	for _, path := range h.Selection {
		enabled[NameFromPath(path)] = true
	}
	runningNames := make(map[string]bool, len(running))
	for _, path := range running {
		runningNames[NameFromPath(path)] = true
	}
	pulled, err := h.Closure()
	if err != nil {
		return nil, err
	}

	var out []Status
	for _, m := range h.Modules {
		on, live := enabled[m.Name], runningNames[m.Name]
		st := Status{Module: m, State: stateOf(on, live)}
		switch {
		case !on:
			if by, ok := pulled[m.Name]; ok {
				st.State, st.PulledBy = Pulled, by
			}
		case live && baseline != "":
			changed, err := m.changedFrom(baseline)
			if err != nil {
				return nil, err
			}
			if changed {
				st.State = Modified
			}
		}
		if st.Inputs, err = m.inputs(); err != nil {
			return nil, err
		}
		out = append(out, st)
	}

	seen := map[string]bool{}
	for _, path := range running {
		name := NameFromPath(path)
		if _, ok := h.Find(name); ok || seen[name] {
			continue
		}
		seen[name] = true
		shown := strings.TrimSuffix(strings.TrimPrefix(path, "./"), "/"+entrypointName)
		out = append(out, Status{Module: Module{Name: name, Path: shown}, State: Removed})
	}
	return out, nil
}

func stateOf(enabled, running bool) State {
	switch {
	case enabled && running:
		return Active
	case enabled:
		return Staged
	case running:
		return Leftover
	default:
		return Off
	}
}

// StagedPath is where the module sits in the staged tree: a shadow takes its
// original's location, under its own file or folder name.
func (m Module) StagedPath() string {
	if m.Shadows == "" {
		return m.Path
	}
	return filepath.Join(filepath.Dir(m.Shadows), filepath.Base(m.Path))
}

// changedFrom reports whether the module's files (symlinks dereferenced, as
// staging copies them) differ from its copy under baseline. A module missing
// on the baseline side is changed. A bundle's modules/ folder is not part of
// it: a submodule's change marks the submodule.
func (m Module) changedFrom(baseline string) (bool, error) {
	skip := ""
	if IsBundle(m.Abs) {
		skip = bundleModulesDir
	}
	src, err := readFiles(m.Abs, skip)
	if err != nil {
		return false, err
	}
	dst, err := readFiles(filepath.Join(baseline, m.StagedPath()), skip)
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

// readFiles returns the contents of root keyed by path relative to root;
// a single file is keyed ".". Symlinks are followed. skipTop, when non-empty,
// names a top-level entry of root left out.
func readFiles(root, skipTop string) (map[string][]byte, error) {
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

// inputs returns the sorted names of the flake inputs the module declares.
func (m Module) inputs() ([]string, error) {
	files, err := m.Files()
	if err != nil {
		return nil, err
	}
	decls, err := nix.InputDecls(files...)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, d := range decls {
		set[d.Name] = true
	}
	var names []string
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
