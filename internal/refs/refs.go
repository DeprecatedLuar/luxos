// Package refs is the single owner of what a module file under
// CONFIG_DIR/modules may reference — paths, and luxos.modules names — as
// internal/imports is for host entrypoints. It owns the domain logic
// (owners, closures, dependents, boundary violations); all Nix syntax
// reading and editing goes through internal/nixsrc. Every function here
// takes absolute file paths.
//
// Bundles (a folder unit with a modules/ subdirectory) add three boundary
// rules: a bundle's own files may not reference paths inside its modules/,
// luxos.modules may not name a submodule, and a selected submodule needs its
// bundle selected too.
package refs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nixsrc"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

const entrypointName = "default.nix"

const localName = "local"

const modulesDirName = "modules"

const localPrefix = "local/"

const nameSep = "/"

type Violation struct {
	File, Message string
}

// New == "" means the element was removed rather than renamed.
type Change struct {
	File, Old, New string
}

//──[Public API]──────────────────────────────────────────────────────────────

// Owner returns the owning folder-module directory of file (absolute path,
// may be a file or a directory), or "" when none exists — the nearest
// ancestor strictly below modulesDir that has its own default.nix.
// A file submodule (directly or through categories under a bundle's
// modules/) has no owner, so it may reference no paths.
// Comparison is purely lexical (string prefix, per §3): never resolved with
// EvalSymlinks.
func Owner(modulesDir, file string) string {
	root := strings.TrimSuffix(modulesDir, "/")
	d := filepath.Dir(file)
	for strings.HasPrefix(d, root+"/") {
		if fi, err := os.Stat(filepath.Join(d, entrypointName)); err == nil && !fi.IsDir() {
			return d
		}
		if filepath.Base(d) == modulesDirName && units.IsBundle(filepath.Dir(d)) {
			return ""
		}
		d = filepath.Dir(d)
	}
	return ""
}

// Validate checks only what the host actually builds: starting from roots
// (the host entrypoint's import paths, relative to modulesDir), it follows
// every luxos.modules name to its unit, transitively, and collects every
// boundary, dynamic-path, call-shape and unresolved-name violation in that
// closure — not just the first. Bundle rules: a bundle's own files may not
// reference paths inside its modules/, luxos.modules may not name a
// submodule, and a root submodule needs its bundle among the roots. A unit nothing reaches is never checked:
// importing it makes it part of the closure on the next run. A file unit
// contributes itself, a folder unit every *.nix beneath it. err is returned
// for a root that resolves to no unit, or for I/O or parser-lookup failure
// (e.g. nix-instantiate missing); a file that itself fails to parse is
// reported as a Violation instead.
func Validate(modulesDir string, us []units.Unit, roots []string) ([]Violation, error) {
	root := strings.TrimSuffix(modulesDir, "/")

	var queue []string
	seen := make(map[string]bool)
	enqueue := func(unitPath string) {
		if !seen[unitPath] {
			seen[unitPath] = true
			queue = append(queue, unitPath)
		}
	}
	rootNames := make(map[string]bool, len(roots))
	for _, r := range roots {
		rootNames[units.NameFromPath(r)] = true
	}

	var violations []Violation
	for _, r := range roots {
		name := units.NameFromPath(r)
		unitPath, ok := units.Resolve(us, name)
		if !ok {
			return nil, fmt.Errorf("import ./%s does not resolve to any module under %s/", r, modulesDirName)
		}
		if parent := units.Parent(name); parent != "" && !rootNames[parent] {
			violations = append(violations, Violation{
				File:    r,
				Message: fmt.Sprintf("selects submodule '%s' but not its bundle '%s'; enable it with: luxos module enable %s", name, parent, parent),
			})
		}
		enqueue(unitPath)
	}

	for len(queue) > 0 {
		unitPath := queue[0]
		queue = queue[1:]
		unitName := units.NameFromPath(unitPath)

		files, err := UnitFiles(filepath.Join(root, unitPath))
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				return nil, err
			}

			static, dynamic, perr := nixsrc.PathsAndDynamic(file)
			if perr != nil {
				violations = append(violations, Violation{File: rel, Message: "failed to parse"})
				continue
			}
			names, callViolations, perr := nixsrc.ModuleNames(file)
			if perr != nil {
				violations = append(violations, Violation{File: rel, Message: "failed to parse"})
				continue
			}

			owner := Owner(root, file)

			ownerModules := filepath.Join(owner, modulesDirName)
			for _, p := range static {
				if owner == "" || !(p == owner || strings.HasPrefix(p, owner+"/")) {
					violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("references %s outside its module", p)})
					continue
				}
				if units.IsBundle(owner) && (p == ownerModules || strings.HasPrefix(p, ownerModules+"/")) {
					violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("references %s inside its bundle's modules/ (submodules are selected in the host's modules.nix)", p)})
				}
			}

			for _, p := range dynamic {
				violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("uses a dynamic path (%s)", p)})
			}

			for _, v := range callViolations {
				violations = append(violations, Violation{File: rel, Message: v})
			}
			for _, name := range names {
				if name == unitName {
					violations = append(violations, Violation{
						File:    rel,
						Message: fmt.Sprintf("luxos.modules: module references its own name '%s'", name),
					})
					continue
				}
				depUnit, ok := units.Find(us, name)
				dep := depUnit.Path
				if !ok {
					violations = append(violations, Violation{
						File:    rel,
						Message: fmt.Sprintf("luxos.modules: '%s' does not resolve to any module under %s/", name, modulesDirName),
					})
					continue
				}
				if strings.Contains(name, nameSep) {
					violations = append(violations, Violation{
						File:    rel,
						Message: fmt.Sprintf("luxos.modules: '%s' is a submodule, private to its bundle; reference '%s' instead", name, units.Top(name)),
					})
					continue
				}
				if !strings.HasPrefix(unitPath, localPrefix) && strings.HasPrefix(dep, localPrefix) && depUnit.Shadows == "" {
					violations = append(violations, Violation{
						File:    rel,
						Message: fmt.Sprintf("luxos.modules: shared module references local module '%s'", name),
					})
				}
				enqueue(dep)
			}
		}
	}

	sort.SliceStable(violations, func(i, j int) bool { return violations[i].File < violations[j].File })
	return violations, nil
}

// Closure returns, for every unit name reached from roots via a
// "luxos.modules [ ... ]" call (transitively), the sorted set of unit
// names that reference it — used by `module`/`user list` to show a
// module that isn't directly selected but is still pulled in by one
// that is. Unlike Validate, it is display-only and best-effort: an
// unresolvable name, a file that fails to parse, or a bad root is
// silently skipped rather than reported, since a broken config is
// already caught by Validate at rebuild time. A root itself is never a
// key unless something else also references it.
func Closure(modulesDir string, us []units.Unit, roots []string) (map[string][]string, error) {
	root := strings.TrimSuffix(modulesDir, "/")

	var queue []string
	seen := make(map[string]bool)
	enqueue := func(unitPath string) {
		if !seen[unitPath] {
			seen[unitPath] = true
			queue = append(queue, unitPath)
		}
	}
	for _, r := range roots {
		if unitPath, ok := units.Resolve(us, units.NameFromPath(r)); ok {
			enqueue(unitPath)
		}
	}

	pullers := make(map[string]map[string]bool)
	for len(queue) > 0 {
		unitPath := queue[0]
		queue = queue[1:]
		unitName := units.NameFromPath(unitPath)

		files, err := UnitFiles(filepath.Join(root, unitPath))
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil || !strings.Contains(string(raw), "luxos") {
				continue
			}

			names, violations, err := nixsrc.ModuleNames(file)
			if err != nil || len(violations) > 0 {
				continue
			}
			for _, name := range names {
				dep, ok := units.Resolve(us, name)
				if !ok {
					continue
				}
				if pullers[name] == nil {
					pullers[name] = make(map[string]bool)
				}
				pullers[name][unitName] = true
				enqueue(dep)
			}
		}
	}

	out := make(map[string][]string, len(pullers))
	for name, set := range pullers {
		list := make([]string, 0, len(set))
		for p := range set {
			list = append(list, p)
		}
		sort.Strings(list)
		out[name] = list
	}
	return out, nil
}

// Dependents returns every module file under modulesDir (root "local"
// entry skipped, L5) plus every localModulesDirs entry, whose
// luxos.modules list contains name. Read-only; a file whose luxos.modules
// use isn't the recognized shape (or that fails to parse) is silently
// skipped — it just can't be a hit. A missing localModulesDirs entry is
// silently skipped too.
func Dependents(modulesDir string, localModulesDirs []string, name string) ([]string, error) {
	root := strings.TrimSuffix(modulesDir, "/")
	files, err := findAllNix(root, localName)
	if err != nil {
		return nil, err
	}
	for _, d := range localModulesDirs {
		if _, err := os.Stat(d); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		lf, err := findAllNix(d, "")
		if err != nil {
			return nil, err
		}
		files = append(files, lf...)
	}
	sort.Strings(files)
	rootDefault := filepath.Join(root, entrypointName)

	var out []string
	for _, file := range files {
		if file == rootDefault {
			continue
		}
		names, _, err := nixsrc.ModuleNames(file)
		if err != nil {
			continue
		}
		for _, n := range names {
			if n == name {
				out = append(out, file)
				break
			}
		}
	}
	return out, nil
}

// Retarget is the single writer of module files (#30, §3 "Rewrite"): in
// every module file under modulesDir whose luxos.modules list contains
// name, rewrite it to newName if given, else delete it from the list.
// Every dependent's call shape is validated (nixsrc.Names, which errors on an
// unrecognized use of luxos) before any file is touched, so a bad shape
// anywhere refuses the whole operation before anything changes (item 20).
// nixsrc.RetargetModuleName refuses any candidate that differs from the
// original by more than the one substitution.
func Retarget(modulesDir string, localModulesDirs []string, name, newName string) ([]Change, error) {
	files, err := Dependents(modulesDir, localModulesDirs, name)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Validate shape everywhere first — Names errors naming the file.
	for _, file := range files {
		if _, err := nixsrc.Names(file); err != nil {
			return nil, err
		}
	}

	var changes []Change
	for _, file := range files {
		changed, err := nixsrc.RetargetModuleName(file, name, newName)
		if err != nil {
			return nil, err
		}
		if changed {
			changes = append(changes, Change{File: file, Old: name, New: newName})
		}
	}

	return changes, nil
}

//──[private: tree walk]──────────────────────────────────────────────────────

func UnitFiles(unit string) ([]string, error) {
	info, err := os.Stat(unit)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{unit}, nil
	}
	if units.IsBundle(unit) {
		return findAllNix(unit, modulesDirName)
	}
	return findAllNix(unit, "")
}

// findAllNix returns every *.nix file under root (following symlinks, like
// `find -L root -type f -name '*.nix'`), sorted in byte order. A broken
// symlink or vanished entry is skipped. skipRootEntry, when non-empty, is
// the name of one entry at root's own top level (not any nested occurrence)
// that is skipped entirely rather than walked — used to keep modulesDir's
// reserved "local" link (L5) out of a walk rooted at modulesDir itself.
func findAllNix(root, skipRootEntry string) ([]string, error) {
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if dir == root && skipRootEntry != "" && e.Name() == skipRootEntry {
				continue
			}
			p := filepath.Join(dir, e.Name())
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if strings.HasSuffix(e.Name(), ".nix") {
				out = append(out, p)
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
