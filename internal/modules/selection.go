package modules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// selectionAttr is the only top-level name a selection may set.
const selectionAttr = "imports"

const userMachineFormat = "'%s' belongs to user %s: a user's modules are enabled in its own selection"

// bundleModulesInfix separates a bundle's path from its submodules' in an
// import path.
const bundleModulesInfix = "/modules/"

// New == "" means the line was removed.
type Change struct {
	File, Old, New string
}

// SelectionFile is one selection on disk and the scope its lines are relative to.
type SelectionFile struct {
	Scope, File string
}

// Move is a selection line put into the selection of the scope its unit lives in.
type Move struct {
	Name, From, To string
}

// ReadSelection returns the modules-relative paths of the lines of file, the
// selection of scope. It is a hard error if file doesn't have exactly one
// recognizable imports block.
func ReadSelection(file, scope string) ([]string, error) {
	lines, ok, err := nix.ListImports(file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nix.ShapeError(file)
	}
	return pathsOf(scope, lines), nil
}

// pathsOf converts selection lines of scope to modules-relative paths.
func pathsOf(scope string, lines []string) []string {
	paths := make([]string, len(lines))
	for i, l := range lines {
		paths[i] = config.PathOf(scope, l)
	}
	return paths
}

// CheckSelection fails when file sets anything but its imports list, or when
// that list holds an entry that is not a ./path line of its own.
func CheckSelection(file string) error {
	shape, err := nix.ReadSelectionShape(file)
	if err != nil {
		return err
	}
	var others []string
	for _, a := range shape.Attrs {
		if a != selectionAttr {
			others = append(others, a)
		}
	}
	if len(others) > 0 {
		return fmt.Errorf("%s may hold only its imports list and comments; it also sets %s\n  move them into a module: luxos module add <category/name>", file, strings.Join(others, ", "))
	}
	lines, err := ReadSelection(file, config.ScopeShared)
	if err != nil {
		return err
	}
	if shape.Imports != len(lines) {
		return fmt.Errorf("%s: imports holds %d entries but %d are ./path lines; write one ./path per line", file, shape.Imports, len(lines))
	}
	return nil
}

// AllSelections returns every host's, every machine's and every user's
// selection, in that order, each sorted by folder name.
func AllSelections(modulesDir, hostsDir, machinesDir string) ([]SelectionFile, error) {
	var out []SelectionFile
	for _, root := range []struct{ scope, dir string }{{config.ScopeHost, hostsDir}, {config.ScopeMachine, machinesDir}} {
		names, err := scopeFolders(root.scope, root.dir)
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			out = append(out, SelectionFile{Scope: root.scope, File: config.SelectionPath(filepath.Join(root.dir, n))})
		}
	}
	users, err := UserSelections(modulesDir)
	if err != nil {
		return nil, err
	}
	return append(out, users...), nil
}

// UserSelections returns the selection of every user unit under modulesDir
// holding one, by name.
func UserSelections(modulesDir string) ([]SelectionFile, error) {
	dir := filepath.Join(modulesDir, usersCategory)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SelectionFile
	for _, e := range entries {
		unit := filepath.Join(dir, e.Name())
		file := config.UserSelectionPath(unit)
		if isFile(filepath.Join(unit, entrypointName)) && isFile(file) {
			out = append(out, SelectionFile{Scope: config.UserScope(e.Name()), File: file})
		}
	}
	return out, nil
}

// userOfScope returns the user a selection scope belongs to, "" for a host or machine scope.
func userOfScope(scope string) string {
	rest, ok := strings.CutPrefix(scope, usersCategory+nameSep)
	if !ok {
		return ""
	}
	name, _ := strings.CutSuffix(rest, nameSep+bundleModulesDir)
	return name
}

func scopeFolders(scope, dir string) ([]string, error) {
	if scope == config.ScopeHost {
		return config.Hosts(dir)
	}
	return config.Machines(dir)
}

// Importers returns every file of files with a line whose name (from its
// path, per NameFromPath) matches name, stale paths included. Read-only. A
// file that isn't recognizable is silently skipped (same tolerance as
// Retarget and Heal: it just can't be a hit).
func Importers(files []SelectionFile, name string) ([]string, error) {
	var out []string
	for _, f := range files {
		lines, ok, err := nix.ListImports(f.File)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, p := range pathsOf(f.Scope, lines) {
			if NameFromPath(p) == name {
				out = append(out, f.File)
				break
			}
		}
	}
	return out, nil
}

// RetargetSelections is the writer of selections: rewrite every import line
// of files whose name matches name to newPath (modules-relative), or delete
// it when newPath is "". Returns every change made; a no-op line (already at
// newPath) is left untouched and unreported, so a second run returns no
// changes. A file that isn't recognizable is left untouched.
func RetargetSelections(files []SelectionFile, name, newPath string) ([]Change, error) {
	changes, _, err := retarget(files, name, newPath)
	return changes, err
}

func retarget(files []SelectionFile, name, newPath string) ([]Change, []string, error) {
	newPath = strings.TrimPrefix(newPath, "./")

	var changes []Change
	var warnings []string

	for _, f := range files {
		match := func(line string) bool { return NameFromPath(config.PathOf(f.Scope, line)) == name }
		newLine := ""
		if newPath != "" {
			newLine = config.LineOf(f.Scope, newPath)
		}
		old, ok, err := nix.RetargetImports(f.File, match, newLine)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s does not have exactly one recognizable imports block, skipping", f.File))
			continue
		}
		for _, o := range pathsOf(f.Scope, old) {
			changes = append(changes, Change{File: f.File, Old: o, New: newPath})
		}
	}

	return changes, warnings, nil
}

// RetargetPrefix rewrites every import line of files that selects a submodule
// of the bundle at oldPrefix (a path starting with oldPrefix + "/modules/") so
// the bundle prefix becomes newPrefix, or deletes the line when newPrefix is
// "". A file that isn't recognizable is left untouched.
func RetargetPrefix(files []SelectionFile, oldPrefix, newPrefix string) ([]Change, error) {
	var changes []Change
	for _, f := range files {
		lines, ok, err := nix.ListImports(f.File)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		done := make(map[string]bool)
		for _, path := range pathsOf(f.Scope, lines) {
			if !strings.HasPrefix(path, oldPrefix+bundleModulesInfix) || done[path] {
				continue
			}
			done[path] = true
			newPath := ""
			if newPrefix != "" {
				newPath = newPrefix + strings.TrimPrefix(path, oldPrefix)
			}
			line := config.LineOf(f.Scope, path)
			newLine := ""
			if newPath != "" {
				newLine = config.LineOf(f.Scope, newPath)
			}
			old, _, err := nix.RetargetImports(f.File, func(l string) bool { return l == line }, newLine)
			if err != nil {
				return nil, err
			}
			for _, o := range pathsOf(f.Scope, old) {
				changes = append(changes, Change{File: f.File, Old: o, New: newPath})
			}
		}
	}
	return changes, nil
}

// Heal first moves every cross-scope line of the active host's and this
// machine's selections (a ../machine/x line in the host's, a ../host/x line
// in the machine's) into the other one. It then checks every selection of
// every host and machine folder against the shared units plus that folder's
// own: a line naming no file that resolves by name is rewritten, in every
// selection for a shared unit and in its own for a scoped one. A name
// resolving to nothing fails on the active host's and this machine's
// selections (prune removes it instead) and is left alone elsewhere, as are
// cross-scope lines of other folders. A folder whose own Walk fails errors
// the whole call when it is active, otherwise it is skipped. Heal is
// idempotent. A user's selection resolves against the shared units, and its
// unresolvable lines fail only when the active host or machine selects that
// user.
func Heal(modulesDir, hostsDir, machinesDir, activeHost, machineDir string, prune bool) ([]Move, []Change, []string, error) {
	activeHostSel := config.SelectionPath(filepath.Join(hostsDir, activeHost))
	activeMachineSel := ""
	if machineDir != "" {
		activeMachineSel = config.SelectionPath(machineDir)
	}

	moves, err := moveCrossScope(activeHostSel, activeMachineSel)
	if err != nil {
		return nil, nil, nil, err
	}

	files, err := AllSelections(modulesDir, hostsDir, machinesDir)
	if err != nil {
		return nil, nil, nil, err
	}

	activeUsers, err := activeUsersOf(modulesDir, activeHostSel, activeMachineSel)
	if err != nil {
		return nil, nil, nil, err
	}

	var changes []Change
	var warnings []string
	var unresolved []string

	// Retargets already applied, keyed by name for a shared unit and by file
	// and name for a scoped one: retarget itself reaches every relevant file,
	// so a name on more than one broken line is retargeted once.
	handled := make(map[string]bool)

	for _, f := range files {
		user := userOfScope(f.Scope)
		active := f.File == activeHostSel || f.File == activeMachineSel || activeUsers[user]
		folderModules := filepath.Join(filepath.Dir(filepath.Dir(f.File)), config.ScopeModulesDir)

		var us []Module
		switch {
		case user != "":
			us, _, err = Walk(modulesDir, "", "")
		case f.Scope == config.ScopeHost:
			us, _, err = Walk(modulesDir, folderModules, "")
		default:
			us, _, err = Walk(modulesDir, "", folderModules)
		}
		if err != nil {
			if active {
				return nil, nil, nil, err
			}
			continue
		}

		lines, ok, err := nix.ListImports(f.File)
		if err != nil {
			return nil, nil, nil, err
		}
		if !ok {
			if active {
				warnings = append(warnings, fmt.Sprintf("%s does not have exactly one recognizable imports block, skipping heal", f.File))
			}
			continue
		}

		for _, line := range lines {
			p := config.PathOf(f.Scope, line)
			if crossScope(f.Scope, p) {
				continue
			}
			rest, cut := strings.CutPrefix(p, f.Scope+nameSep)
			fullPath := filepath.Join(modulesDir, p)
			if cut && (f.Scope == config.ScopeHost || f.Scope == config.ScopeMachine) {
				fullPath = filepath.Join(folderModules, rest)
			}
			if _, err := os.Stat(fullPath); err == nil {
				continue
			}

			name := NameFromPath(p)
			if found, ok := Find(us, name); ok {
				targets, key := []SelectionFile{f}, f.File+"\x00"+name
				if found.Scope == config.ScopeShared {
					targets, key = files, name
				}
				if handled[key] {
					continue
				}
				handled[key] = true
				rc, rw, err := retarget(targets, name, found.Path)
				if err != nil {
					return nil, nil, nil, err
				}
				changes = append(changes, rc...)
				warnings = append(warnings, rw...)
				continue
			}

			if !active {
				continue
			}
			if prune {
				if err := nix.RemoveImport(f.File, line); err != nil {
					return nil, nil, nil, err
				}
				changes = append(changes, Change{File: f.File, Old: p, New: ""})
			} else {
				unresolved = append(unresolved, fmt.Sprintf("%s: %s does not exist and '%s' does not resolve to any module", f.File, line, name))
			}
		}
	}

	if len(unresolved) > 0 {
		msg := strings.Join(unresolved, "\n") + "\n  Fix the import by hand, or rerun with --prune to remove it."
		return moves, changes, warnings, errors.New(msg)
	}

	return moves, changes, warnings, nil
}

// activeUsersOf returns the users the active host's and this machine's
// selections name ("" leaves a selection out), resolved as Load resolves them.
// An unrecognizable selection names none.
func activeUsersOf(modulesDir, hostSel, machineSel string) (map[string]bool, error) {
	scopeModules := func(sel string) string {
		if sel == "" {
			return ""
		}
		return filepath.Dir(sel)
	}
	units, _, err := Walk(modulesDir, scopeModules(hostSel), scopeModules(machineSel))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, f := range []SelectionFile{{config.ScopeHost, hostSel}, {config.ScopeMachine, machineSel}} {
		if f.File == "" {
			continue
		}
		lines, ok, err := nix.ListImports(f.File)
		if err != nil {
			return nil, err
		}
		if ok {
			paths = append(paths, pathsOf(f.Scope, lines)...)
		}
	}
	active := map[string]bool{}
	for _, name := range usersNamedBy(units, paths) {
		active[name] = true
	}
	return active, nil
}

// crossScope reports whether path p, written in a selection of scope, names
// a unit of the other scope.
func crossScope(scope, p string) bool {
	switch scope {
	case config.ScopeHost:
		return strings.HasPrefix(p, MachinePrefix)
	case config.ScopeMachine:
		return strings.HasPrefix(p, HostPrefix)
	}
	return false
}

// moveCrossScope moves each line of hostFile naming a machine unit into
// machineFile, and each line of machineFile naming a host unit into hostFile.
// machineFile "" leaves the machine side out.
func moveCrossScope(hostFile, machineFile string) ([]Move, error) {
	if machineFile == "" {
		return nil, nil
	}
	var moves []Move
	for _, pair := range []struct {
		from, to           string
		fromScope, toScope string
	}{
		{hostFile, machineFile, config.ScopeHost, config.ScopeMachine},
		{machineFile, hostFile, config.ScopeMachine, config.ScopeHost},
	} {
		lines, ok, err := nix.ListImports(pair.from)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, line := range lines {
			p := config.PathOf(pair.fromScope, line)
			if !crossScope(pair.fromScope, p) {
				continue
			}
			if err := nix.RemoveImport(pair.from, line); err != nil {
				return nil, err
			}
			present, err := selectionHas(pair.to, pair.toScope, p)
			if err != nil {
				return nil, err
			}
			if !present {
				if err := nix.AddImport(pair.to, config.LineOf(pair.toScope, p)); err != nil {
					return nil, err
				}
			}
			moves = append(moves, Move{Name: NameFromPath(p), From: pair.from, To: pair.to})
		}
	}
	return moves, nil
}

// selectionHas reports whether file, a selection of scope, has a line for path p.
func selectionHas(file, scope, p string) (bool, error) {
	paths, err := ReadSelection(file, scope)
	if err != nil {
		return false, err
	}
	for _, have := range paths {
		if have == p {
			return true, nil
		}
	}
	return false, nil
}

// Enable adds name's line to the selection it belongs in: a machine unit's
// to the machine selection, any other to the host's, or to the machine's
// when toMachine (a shared unit only). A user's submodule goes to its user's
// selection, created when the user has none. Reports false when already there.
func Enable(h *Host, name string, toMachine bool) (bool, error) {
	m, err := h.Lookup(name)
	if err != nil {
		return false, err
	}
	if user := UserOf(m); user != "" && m.Role() != RoleUser {
		if toMachine {
			return false, fmt.Errorf(userMachineFormat, name, user)
		}
		sel, err := h.ensureUserSelection(user)
		if err != nil {
			return false, err
		}
		return addLine(sel, m)
	}
	scope := config.ScopeHost
	switch {
	case m.Scope == config.ScopeMachine:
		scope = config.ScopeMachine
	case toMachine && m.Scope == config.ScopeHost:
		return false, fmt.Errorf("'%s' is a host module; the machine selection holds only shared and machine modules", name)
	case toMachine:
		scope = config.ScopeMachine
	}
	sel := h.SelectionOf(scope)
	if sel == nil {
		return false, fmt.Errorf("no %s selection: run luxos rebuild build first", scope)
	}
	return addLine(sel, m)
}

// ensureUserSelection returns the selection of user, creating an empty one
// when the user has no selection file. An existing file that does not read is
// an error and is never rewritten.
func (h *Host) ensureUserSelection(user string) (*Selection, error) {
	if sel := h.UserSelection(user); sel != nil {
		return sel, nil
	}
	dir := filepath.Join(h.ModulesDir, usersCategory, user)
	file := config.UserSelectionPath(dir)
	scope := config.UserScope(user)
	var paths []string
	if _, err := os.Lstat(file); err == nil {
		if paths, err = ReadSelection(file, scope); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if err := config.CreateUserSelection(dir, nil); err != nil {
		return nil, err
	}
	h.Users = append(h.Users, Selection{Scope: scope, File: file, Paths: paths})
	return &h.Users[len(h.Users)-1], nil
}

// addLine adds m's line to sel; false when sel already names m.
func addLine(sel *Selection, m Module) (bool, error) {
	for _, p := range sel.Paths {
		if NameFromPath(p) == m.Name {
			return false, nil
		}
	}
	if err := nix.AddImport(sel.File, config.LineOf(sel.Scope, m.Path)); err != nil {
		return false, err
	}
	sel.Paths = append(sel.Paths, m.Path)
	return true, nil
}

// Disable removes name's line from every selection holding it and returns
// those files.
func Disable(h *Host, name string) ([]string, error) {
	var files []string
	for _, group := range [][]Selection{h.Selections, h.Users} {
		for i := range group {
			sel := &group[i]
			kept := sel.Paths[:0]
			removed := false
			for _, p := range sel.Paths {
				if NameFromPath(p) != name {
					kept = append(kept, p)
					continue
				}
				if err := nix.RemoveImport(sel.File, config.LineOf(sel.Scope, p)); err != nil {
					return files, err
				}
				removed = true
			}
			sel.Paths = kept
			if removed {
				files = append(files, sel.File)
			}
		}
	}
	return files, nil
}
