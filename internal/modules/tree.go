// Package modules is one host's view of the module tree: the shared units, the
// host's own local units, its selection, the rules between modules and their
// state. Every directory is a parameter; nothing here resolves paths or prints.
package modules

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

const (
	// HostPrefix and MachinePrefix start the paths of a host's and a machine's own units.
	HostPrefix    = config.ScopeHost + nameSep
	MachinePrefix = config.ScopeMachine + nameSep

	// usersCategory holds the units that are luxos users.
	usersCategory = config.UsersDir
)

// entrypointName is the file name that turns a directory into a unit (and
// that a walk root's own copy is skipped for).
const entrypointName = config.DefaultFile

// reservedRoot are the entries of modules/ that are never shared units.
var reservedRoot = map[string]bool{entrypointName: true, config.HostLinkName: true, config.MachineLinkName: true}

// scopeOrder is shadow precedence, highest first.
var scopeOrder = []string{config.ScopeMachine, config.ScopeHost, config.ScopeShared}

// bundleModulesDir is the subdirectory of a folder unit that holds its
// submodules; the unit is then a bundle.
const bundleModulesDir = "modules"

const unknownModuleFormat = "unknown module '%s'"

const nameSep = "/"

// nixExt marks a file unit.
const nixExt = ".nix"

// Module is one unit: a <name>.nix file or a folder with its own default.nix.
// Path is relative to the modules dir, with no leading "./" and the scope
// prefix ("host/", "machine/") on a unit of a scope. A submodule's Name is
// qualified by its bundle ("eduardo/git"). Shadows is the modules-relative path
// of the lowest-scope unit a unit hides, or, for a submodule of a shadowing
// bundle, the path it takes when the bundle is staged in the hidden bundle's
// place; "" otherwise. Abs is the location on disk. Scope is
// config.ScopeShared, ScopeHost or ScopeMachine.
type Module struct {
	Name    string
	Path    string
	Shadows string
	Abs     string
	Scope   string
}

// Selection is one selection file with its modules-relative paths.
type Selection struct {
	Scope, File string
	Paths       []string
}

// Host is one host's view of the module tree.
type Host struct {
	Name, ModulesDir, HostDir, MachineDir string      // MachineDir "" when unknown
	Modules                               []Module    // sorted by name; hidden units removed
	Hidden                                []Module    // units a higher scope hides
	Selections                            []Selection // host first, then machine when MachineDir != ""
	Users                                 []Selection // the selection of every user unit holding one, by name
}

// Load reads one host's view: the shared units, the host's and the
// machine's own units, and both selections. machineDir "" leaves the machine
// scope out. Every user unit's selection is read into Users; one that does
// not read is an error only for a user the host's or the machine's selection names,
// and is left out otherwise. Nothing is read through modules/host or modules/machine.
func Load(modulesDir, hostDir, machineDir string) (*Host, error) {
	machineModules := ""
	if machineDir != "" {
		machineModules = filepath.Join(machineDir, config.ScopeModulesDir)
	}
	units, hidden, err := Walk(modulesDir, filepath.Join(hostDir, config.ScopeModulesDir), machineModules)
	if err != nil {
		return nil, err
	}
	h := &Host{Name: filepath.Base(hostDir), ModulesDir: modulesDir, HostDir: hostDir, MachineDir: machineDir, Modules: units, Hidden: hidden}
	scopes := []struct{ scope, dir string }{{config.ScopeHost, hostDir}, {config.ScopeMachine, machineDir}}
	for _, s := range scopes {
		if s.dir == "" {
			continue
		}
		file := config.SelectionPath(s.dir)
		paths, err := ReadSelection(file, s.scope)
		if err != nil {
			return nil, err
		}
		h.Selections = append(h.Selections, Selection{Scope: s.scope, File: file, Paths: paths})
	}
	selected := map[string]bool{}
	for _, name := range h.selectedUserNames() {
		selected[name] = true
	}
	for _, m := range units {
		if m.Role() != RoleUser {
			continue
		}
		file := config.UserSelectionPath(m.Abs)
		if !isFile(file) {
			continue
		}
		scope := config.UserScope(m.Name)
		paths, err := ReadSelection(file, scope)
		if err != nil {
			if selected[m.Name] {
				return nil, err
			}
			continue
		}
		h.Users = append(h.Users, Selection{Scope: scope, File: file, Paths: paths})
	}
	return h, nil
}

// Find returns the module called name.
func (h *Host) Find(name string) (Module, bool) {
	return Find(h.Modules, name)
}

// Lookup is Find, failing on a name that names no module.
func (h *Host) Lookup(name string) (Module, error) {
	m, ok := h.Find(name)
	if !ok {
		return m, fmt.Errorf(unknownModuleFormat, name)
	}
	return m, nil
}

// Selection is every selected path, each once: the host's lines, then the
// machine's, then the lines of each user they select.
func (h *Host) Selection() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, s := range h.Selections {
		for _, p := range s.Paths {
			add(p)
		}
	}
	for _, p := range append([]string(nil), out...) {
		for _, up := range h.UserPaths(p) {
			add(up)
		}
	}
	return out
}

// UserSelection returns the selection of user name, nil when absent.
func (h *Host) UserSelection(name string) *Selection {
	scope := config.UserScope(name)
	for i := range h.Users {
		if h.Users[i].Scope == scope {
			return &h.Users[i]
		}
	}
	return nil
}

// UserPaths returns the selection paths of the user unit p names, nil when p
// names no user or the user has no selection.
func (h *Host) UserPaths(p string) []string {
	m, ok := h.Find(NameFromPath(p))
	if !ok || m.Role() != RoleUser {
		return nil
	}
	if sel := h.UserSelection(m.Name); sel != nil {
		return sel.Paths
	}
	return nil
}

// SelectedUsers returns the selections of the users the host's and the
// machine's selections name.
func (h *Host) SelectedUsers() []Selection {
	var out []Selection
	for _, name := range h.selectedUserNames() {
		if sel := h.UserSelection(name); sel != nil {
			out = append(out, *sel)
		}
	}
	return out
}

// selectedUserNames returns the users the host's and the machine's
// selections name, each once, in selection order.
func (h *Host) selectedUserNames() []string {
	var paths []string
	for _, s := range h.Selections {
		paths = append(paths, s.Paths...)
	}
	return usersNamedBy(h.Modules, paths)
}

// usersNamedBy returns the users of units that paths name, each once, in order.
func usersNamedBy(units []Module, paths []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range paths {
		m, ok := Find(units, NameFromPath(p))
		if !ok || m.Role() != RoleUser || seen[m.Name] {
			continue
		}
		seen[m.Name] = true
		out = append(out, m.Name)
	}
	return out
}

// SelectionOf returns the selection of scope, nil when absent.
func (h *Host) SelectionOf(scope string) *Selection {
	for i := range h.Selections {
		if h.Selections[i].Scope == scope {
			return &h.Selections[i]
		}
	}
	return nil
}

// Selected returns the selection path naming name (possibly stale).
func (h *Host) Selected(name string) (string, bool) {
	for _, path := range h.Selection() {
		if NameFromPath(path) == name {
			return path, true
		}
	}
	return "", false
}

// walkScope walks one scope's units: the shared tree when scope is
// ScopeShared, otherwise a scope's modules folder, whose units get the scope
// prefix. "" or a missing folder gives none.
func walkScope(dir, scope string) ([]Module, error) {
	if dir == "" {
		return nil, nil
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) && scope != config.ScopeShared {
			return nil, nil
		}
		return nil, err
	}
	skip := map[string]bool{entrypointName: true}
	if scope == config.ScopeShared {
		skip = reservedRoot
	}
	var raw []Module
	if err := walkDir(dir, dir, "", skip, scope == config.ScopeShared, &raw); err != nil {
		return nil, err
	}
	for i := range raw {
		raw[i].Abs = filepath.Join(dir, raw[i].Path)
		raw[i].Scope = scope
		if scope != config.ScopeShared {
			raw[i].Path = scope + nameSep + raw[i].Path
		}
	}
	if err := checkDuplicates(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Walk discovers the units of the shared tree under modulesDir and of the
// host's and machine's modules folders ("" for none). A top-level name in
// more than one scope is won by the highest (machine, host, shared): the
// winner and its submodules get Shadows, the location they take in the
// stage, from the lowest scope's unit of that name; every other scope's unit
// of that name, submodules included, is returned in hidden.
func Walk(modulesDir, hostModulesDir, machineModulesDir string) (units, hidden []Module, err error) {
	dirs := map[string]string{
		config.ScopeShared:  modulesDir,
		config.ScopeHost:    hostModulesDir,
		config.ScopeMachine: machineModulesDir,
	}
	byScope := map[string][]Module{}
	for _, scope := range scopeOrder {
		if byScope[scope], err = walkScope(dirs[scope], scope); err != nil {
			return nil, nil, err
		}
	}

	present := map[string][]string{} // top-level name -> scopes holding it, precedence order
	for _, scope := range scopeOrder {
		for _, u := range byScope[scope] {
			if Parent(u.Name) == "" {
				present[u.Name] = append(present[u.Name], scope)
			}
		}
	}

	hide := map[string]map[string]bool{} // scope -> hidden top-level names
	for name, scopes := range present {
		if len(scopes) < 2 {
			continue
		}
		winner, orig := scopes[0], scopes[len(scopes)-1]
		origUnit, _ := Find(byScope[orig], name)
		winUnit, _ := Find(byScope[winner], name)
		for i, u := range byScope[winner] {
			switch {
			case u.Name == name:
				byScope[winner][i].Shadows = origUnit.Path
			case Top(u.Name) == name:
				byScope[winner][i].Shadows = filepath.Join(filepath.Dir(origUnit.Path), filepath.Base(winUnit.Path), strings.TrimPrefix(u.Path, winUnit.Path))
			}
		}
		for _, s := range scopes[1:] {
			if hide[s] == nil {
				hide[s] = map[string]bool{}
			}
			hide[s][name] = true
		}
	}

	for _, scope := range scopeOrder {
		for _, u := range byScope[scope] {
			if hide[scope][Top(u.Name)] {
				hidden = append(hidden, u)
			} else {
				units = append(units, u)
			}
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	sort.Slice(hidden, func(i, j int) bool { return hidden[i].Path < hidden[j].Path })
	return units, hidden, nil
}

func checkDuplicates(us []Module) error {
	claimants := make(map[string][]string, len(us))
	for _, u := range us {
		claimants[u.Name] = append(claimants[u.Name], u.Path)
	}

	var dupeNames []string
	for name, paths := range claimants {
		if len(paths) > 1 {
			dupeNames = append(dupeNames, name)
		}
	}
	if len(dupeNames) == 0 {
		return nil
	}
	sort.Strings(dupeNames)
	var b strings.Builder
	for i, name := range dupeNames {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "duplicate module name '%s':", name)
		paths := append([]string(nil), claimants[name]...)
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(&b, "\n  - %s", p)
		}
	}
	return errors.New(b.String())
}

// walkDir emits (unsorted, duplicates left in) units found under dir into
// *out. root is the fixed walk root, used to compute relative paths.
// prefix is the qualified-name prefix of the bundle being walked ("" outside
// one). skip names the entries skipped, by path relative to root. shared is
// true when walking the shared tree.
func walkDir(root, dir, prefix string, skip map[string]bool, shared bool, out *[]Module) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())

		// Symlinks are followed; a broken one or a file that vanished
		// mid-walk is skipped.
		info, err := os.Stat(entryPath)
		if err != nil {
			continue
		}

		relPath, err := filepath.Rel(root, entryPath)
		if err != nil {
			return err
		}
		if skip[relPath] {
			continue
		}

		if info.IsDir() {
			if isFile(filepath.Join(entryPath, entrypointName)) {
				name := prefix + entry.Name()
				*out = append(*out, Module{Name: name, Path: relPath, Abs: entryPath})
				if IsBundle(entryPath) {
					if err := walkBundle(root, entryPath, relPath, name, skip, shared, out); err != nil {
						return err
					}
				}
			} else {
				if entry.Name() == bundleModulesDir {
					return fmt.Errorf("%s: a folder named modules is only allowed directly inside a module (a bundle)", relPath)
				}
				if err := walkDir(root, entryPath, prefix, skip, shared, out); err != nil {
					return err
				}
			}
			continue
		}

		if strings.HasSuffix(entry.Name(), nixExt) {
			name := prefix + strings.TrimSuffix(entry.Name(), nixExt)
			*out = append(*out, Module{Name: name, Path: relPath, Abs: entryPath})
		}
	}

	return nil
}

// walkBundle emits the submodules under a bundle's modules/ folder, named
// under the bundle's qualified name. Only a user unit's modules/ may hold a
// default.nix: it is the user's selection, never a unit.
func walkBundle(root, bundleDir, rel, name string, skip map[string]bool, shared bool, out *[]Module) error {
	modulesDir := filepath.Join(bundleDir, bundleModulesDir)
	if isFile(filepath.Join(modulesDir, entrypointName)) {
		if !shared || filepath.Dir(rel) != usersCategory {
			return fmt.Errorf("%s/%s has its own default.nix: a bundle's modules/ folder is not a module", rel, bundleModulesDir)
		}
		withSelection := map[string]bool{filepath.Join(rel, bundleModulesDir, entrypointName): true}
		maps.Copy(withSelection, skip)
		skip = withSelection
	}
	return walkDir(root, modulesDir, name+nameSep, skip, shared, out)
}

// IsBundle reports whether dir is a folder unit with a modules/ subdirectory.
func IsBundle(dir string) bool {
	if !isFile(filepath.Join(dir, entrypointName)) {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, bundleModulesDir))
	return err == nil && fi.IsDir()
}

// Role is what a unit is for.
type Role int

const (
	RolePlain     Role = iota
	RoleUser           // a shared folder unit directly in users/
	RoleBoot           // the machine unit holding this computer's boot loader
	RoleFramework      // a framework module in system/, owned by the binary
)

// bootName is the name of the machine's boot unit.
var bootName = strings.TrimSuffix(config.BootFile, nixExt)

// Role reports what m is for.
func (m Module) Role() Role {
	switch {
	case m.Scope == config.ScopeShared && (m.Path == config.SystemDir || strings.HasPrefix(m.Path, config.SystemDir+nameSep)):
		return RoleFramework
	case m.Scope == config.ScopeMachine && m.Name == bootName:
		return RoleBoot
	case m.Scope == config.ScopeShared && m.Path == usersCategory+nameSep+m.Name:
		fi, err := os.Stat(m.Abs)
		if err == nil && fi.IsDir() {
			return RoleUser
		}
	}
	return RolePlain
}

// UserOf returns the name of the user whose unit m is or lies in, "" when none.
func UserOf(m Module) string {
	if m.Scope != config.ScopeShared {
		return ""
	}
	top := Top(m.Name)
	unit := usersCategory + nameSep + top
	if m.Path != unit && !strings.HasPrefix(m.Path, unit+nameSep) {
		return ""
	}
	if fi, err := os.Stat(filepath.Join(m.Root(), unit)); err != nil || !fi.IsDir() {
		return ""
	}
	return top
}

// isFile reports whether path, symlinks followed, exists and is not a directory.
func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

// Parent is the qualified name of a submodule's bundle, "" for a top-level name.
func Parent(name string) string {
	i := strings.LastIndex(name, nameSep)
	if i < 0 {
		return ""
	}
	return name[:i]
}

// Top is the outermost bundle of a qualified name, or the name itself.
func Top(name string) string {
	top, _, _ := strings.Cut(name, nameSep)
	return top
}

// Find returns the module called name from ms.
func Find(ms []Module, name string) (Module, bool) {
	for _, m := range ms {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}

// NameFromPath derives a unit's name from an import path: strip a trailing
// "/default.nix", else a trailing ".nix", then take the basename. Every
// "modules" segment (not the first or last) makes the segment before it a
// bundle, and the name is qualified by those bundles ("eduardo/git").
func NameFromPath(path string) string {
	trimmed := strings.TrimSuffix(path, nixExt)
	if t := strings.TrimSuffix(path, nameSep+entrypointName); t != path {
		trimmed = t
	}
	segs := strings.Split(strings.TrimRight(trimmed, nameSep), nameSep)
	last := len(segs) - 1
	var parts []string
	for i := 1; i < last; i++ {
		if segs[i] == bundleModulesDir {
			parts = append(parts, segs[i-1])
		}
	}
	return strings.Join(append(parts, segs[last]), nameSep)
}
