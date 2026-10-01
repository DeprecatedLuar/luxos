package modules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// Violation is one module-boundary rule a file breaks.
type Violation struct {
	File, Message string
}

// Owner returns the owning folder-module directory of file (absolute path,
// may be a file or a directory), or "" when none exists: the nearest
// ancestor strictly below root that has its own default.nix.
// A file submodule (directly or through categories under a bundle's
// modules/) has no owner, so it may reference no paths.
// Comparison is purely lexical (string prefix): never resolved with
// EvalSymlinks.
func Owner(root, file string) string {
	root = strings.TrimSuffix(root, "/")
	d := filepath.Dir(file)
	for strings.HasPrefix(d, root+"/") {
		if fi, err := os.Stat(filepath.Join(d, entrypointName)); err == nil && !fi.IsDir() {
			return d
		}
		if filepath.Base(d) == bundleModulesDir && IsBundle(filepath.Dir(d)) {
			return ""
		}
		d = filepath.Dir(d)
	}
	return ""
}

// walk visits, breadth first, every file of every module the selection
// reaches. visit returns the module names the file wants followed; a name
// that resolves to no module is skipped. A module is visited once.
func (h *Host) walk(visit func(m Module, file string) []string) error {
	var queue []Module
	seen := make(map[string]bool)
	enqueue := func(name string) {
		if m, ok := h.Find(name); ok && !seen[m.Path] {
			seen[m.Path] = true
			queue = append(queue, m)
		}
	}
	for _, sel := range h.Selection {
		enqueue(NameFromPath(sel))
	}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		files, err := m.Files()
		if err != nil {
			return err
		}
		for _, file := range files {
			for _, name := range visit(m, file) {
				enqueue(name)
			}
		}
	}
	return nil
}

// Validate checks only what the host actually builds: starting from its
// selection, it follows every luxos.modules name to its module, transitively,
// and collects every boundary, dynamic-path, call-shape and unresolved-name
// violation in that closure, not just the first. Bundle rules: a bundle's
// own files may not reference paths inside its modules/, luxos.modules may
// not name a submodule, and a selected submodule needs its bundle selected
// too. A module nothing reaches is never checked: selecting it makes it part
// of the closure on the next run. err is returned for a selection that
// resolves to no module, or for I/O or parser-lookup failure (e.g.
// nix-instantiate missing); a file that itself fails to parse is reported as
// a Violation instead.
func (h *Host) Validate() ([]Violation, error) {
	selected := make(map[string]bool, len(h.Selection))
	for _, sel := range h.Selection {
		selected[NameFromPath(sel)] = true
	}

	var violations []Violation
	for _, sel := range h.Selection {
		name := NameFromPath(sel)
		if _, ok := h.Find(name); !ok {
			return nil, fmt.Errorf("import ./%s does not resolve to any module under %s/", sel, bundleModulesDir)
		}
		if parent := Parent(name); parent != "" && !selected[parent] {
			violations = append(violations, Violation{
				File:    sel,
				Message: fmt.Sprintf("selects submodule '%s' but not its bundle '%s'; enable it with: luxos module enable %s", name, parent, parent),
			})
		}
	}

	var walkErr error
	err := h.walk(func(m Module, file string) []string {
		root := m.Root()
		rel, err := filepath.Rel(root, file)
		if err != nil {
			walkErr = err
			return nil
		}
		if strings.HasPrefix(m.Path, LocalPrefix) {
			rel = LocalPrefix + rel
		}

		static, dynamic, perr := nix.PathsAndDynamic(file)
		if perr != nil {
			violations = append(violations, Violation{File: rel, Message: "failed to parse"})
			return nil
		}
		names, callViolations, perr := nix.ModuleNames(file)
		if perr != nil {
			violations = append(violations, Violation{File: rel, Message: "failed to parse"})
			return nil
		}

		owner := Owner(root, file)
		ownerModules := filepath.Join(owner, bundleModulesDir)
		for _, p := range static {
			if owner == "" || !(p == owner || strings.HasPrefix(p, owner+"/")) {
				violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("references %s outside its module", p)})
				continue
			}
			if IsBundle(owner) && (p == ownerModules || strings.HasPrefix(p, ownerModules+"/")) {
				violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("references %s inside its bundle's modules/ (submodules are selected in the host's modules.nix)", p)})
			}
		}
		for _, p := range dynamic {
			violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("uses a dynamic path (%s)", p)})
		}
		for _, v := range callViolations {
			violations = append(violations, Violation{File: rel, Message: v})
		}

		var follow []string
		for _, name := range names {
			if name == m.Name {
				violations = append(violations, Violation{
					File:    rel,
					Message: fmt.Sprintf("luxos.modules: module references its own name '%s'", name),
				})
				continue
			}
			dep, ok := h.Find(name)
			if !ok {
				violations = append(violations, Violation{
					File:    rel,
					Message: fmt.Sprintf("luxos.modules: '%s' does not resolve to any module under %s/", name, bundleModulesDir),
				})
				continue
			}
			if strings.Contains(name, nameSep) {
				violations = append(violations, Violation{
					File:    rel,
					Message: fmt.Sprintf("luxos.modules: '%s' is a submodule, private to its bundle; reference '%s' instead", name, Top(name)),
				})
				continue
			}
			if !strings.HasPrefix(m.Path, LocalPrefix) && strings.HasPrefix(dep.Path, LocalPrefix) && dep.Shadows == "" {
				violations = append(violations, Violation{
					File:    rel,
					Message: fmt.Sprintf("luxos.modules: shared module references local module '%s'", name),
				})
			}
			follow = append(follow, name)
		}
		return follow
	})
	if err = errors.Join(err, walkErr); err != nil {
		return nil, err
	}

	sort.SliceStable(violations, func(i, j int) bool { return violations[i].File < violations[j].File })
	return violations, nil
}

// Closure returns, for every module name reached from the selection via a
// "luxos.modules [ ... ]" call (transitively), the sorted set of module
// names that reference it, used by `module`/`user list` to show a module
// that isn't directly selected but is still pulled in by one that is.
// Unlike Validate, it is display-only and best-effort: an unresolvable name,
// a file that fails to parse, or a bad selection entry is silently skipped
// rather than reported, since a broken config is already caught by Validate
// at rebuild time. A selected module is never a key unless something else
// also references it.
func (h *Host) Closure() (map[string][]string, error) {
	pullers := make(map[string]map[string]bool)
	err := h.walk(func(m Module, file string) []string {
		raw, err := os.ReadFile(file)
		if err != nil || !strings.Contains(string(raw), "luxos") {
			return nil
		}
		names, violations, err := nix.ModuleNames(file)
		if err != nil || len(violations) > 0 {
			return nil
		}
		var follow []string
		for _, name := range names {
			if _, ok := h.Find(name); !ok {
				continue
			}
			if pullers[name] == nil {
				pullers[name] = make(map[string]bool)
			}
			pullers[name][m.Name] = true
			follow = append(follow, name)
		}
		return follow
	})
	if err != nil {
		return nil, err
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

// Built returns every module the host builds: the selected ones plus every
// one pulled in through luxos.modules, sorted by path. A selected or pulled
// name that resolves to no module is skipped, as in Closure: a broken
// selection is already caught by Validate at rebuild time.
func (h *Host) Built() ([]Module, error) {
	pulled, err := h.Closure()
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, sel := range h.Selection {
		names[NameFromPath(sel)] = true
	}
	for name := range pulled {
		names[name] = true
	}

	var out []Module
	for name := range names {
		if m, ok := h.Find(name); ok {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
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
		names, _, err := nix.ModuleNames(file)
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

// RetargetRefs is the single writer of module files: in
// every module file under modulesDir whose luxos.modules list contains
// name, rewrite it to newName if given, else delete it from the list.
// Every dependent's call shape is validated (nix.Names, which errors on an
// unrecognized use of luxos) before any file is touched, so a bad shape
// anywhere refuses the whole operation before anything changes (item 20).
// nix.RetargetModuleName refuses any candidate that differs from the
// original by more than the one substitution.
func RetargetRefs(modulesDir string, localModulesDirs []string, name, newName string) ([]Change, error) {
	files, err := Dependents(modulesDir, localModulesDirs, name)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Validate shape everywhere first — Names errors naming the file.
	for _, file := range files {
		if _, err := nix.Names(file); err != nil {
			return nil, err
		}
	}

	var changes []Change
	for _, file := range files {
		changed, err := nix.RetargetModuleName(file, name, newName)
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

// Files returns every *.nix file of the module, sorted: the file itself for
// a file module, every file beneath a folder module (a bundle's submodules
// excluded).
func (m Module) Files() ([]string, error) {
	info, err := os.Stat(m.Abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{m.Abs}, nil
	}
	if IsBundle(m.Abs) {
		return findAllNix(m.Abs, bundleModulesDir)
	}
	return findAllNix(m.Abs, "")
}

// Root returns the modules directory m lives under: the shared modules dir,
// or its host's local modules dir for a local module.
func (m Module) Root() string {
	rel := filepath.ToSlash(strings.TrimPrefix(m.Path, LocalPrefix))
	return strings.TrimSuffix(m.Abs, "/"+rel)
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
