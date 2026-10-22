package modules

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// errBootUnit refuses remove and rename of the machine's boot unit.
var errBootUnit = errors.New("'boot' holds this computer's boot loader and luxos recreates it when missing; edit it with: luxos module edit boot")

// reach is what a remove or rename of one module touches.
type reach struct {
	Selections   []SelectionFile // selections whose lines are rewritten
	UnitDirs     []string        // scope modules folders whose luxos.modules references are rewritten
	SettingsDirs []string        // host or machine folders whose settings follow the unit
	Skipped      []string        // folders whose own unit shadows m, left untouched
}

// scopeOf picks the reach of a remove or rename of m. A host unit reaches
// the active host; a machine unit, this machine. A shared unit reaches every
// host and machine folder except those holding their own unit of that name
// (its bundle's, for a submodule), which keep meaning their own. A user's
// submodule also reaches its user's selection; a user unit never does, since
// its lines move with its folder.
func scopeOf(hostsDir, machinesDir string, h *Host, m Module) (reach, error) {
	own := func(dir, scope string) reach {
		return reach{
			Selections:   []SelectionFile{{Scope: scope, File: config.SelectionPath(dir)}},
			UnitDirs:     []string{filepath.Join(dir, config.ScopeModulesDir)},
			SettingsDirs: []string{dir},
		}
	}
	switch m.Scope {
	case config.ScopeHost:
		s := own(h.HostDir, config.ScopeHost)
		if h.MachineDir != "" {
			s.SettingsDirs = append(s.SettingsDirs, h.MachineDir)
		}
		return s, nil
	case config.ScopeMachine:
		return own(h.MachineDir, config.ScopeMachine), nil
	}

	var s reach
	for _, root := range []struct{ scope, dir string }{{config.ScopeHost, hostsDir}, {config.ScopeMachine, machinesDir}} {
		names, err := scopeFolders(root.scope, root.dir)
		if err != nil {
			return reach{}, err
		}
		for _, n := range names {
			folder := filepath.Join(root.dir, n)
			units, err := walkScope(filepath.Join(folder, config.ScopeModulesDir), root.scope)
			if err != nil {
				return reach{}, fmt.Errorf("%s: %w", folder, err)
			}
			if _, shadows := Find(units, Top(m.Name)); shadows {
				s.Skipped = append(s.Skipped, folder)
				continue
			}
			s.Selections = append(s.Selections, SelectionFile{Scope: root.scope, File: config.SelectionPath(folder)})
			s.UnitDirs = append(s.UnitDirs, filepath.Join(folder, config.ScopeModulesDir))
			s.SettingsDirs = append(s.SettingsDirs, folder)
		}
	}
	if user := UserOf(m); user != "" && m.Role() != RoleUser {
		file := config.UserSelectionPath(filepath.Join(h.ModulesDir, usersCategory, user))
		if isFile(file) {
			s.Selections = append(s.Selections, SelectionFile{Scope: config.UserScope(user), File: file})
		}
	}
	return s, nil
}

// Plan is a remove (NewPath "") or a rename of one unit, worked out and
// checked before anything is written.
type Plan struct {
	Unit       Module
	NewName    string   // qualified new name; "" for a remove
	NewPath    string   // modules-relative new path; "" for a remove
	Shadows    string   // a lower-scope unit the renamed unit shadows
	Importers  []string // selection files naming the unit
	Dependents []string // module files whose luxos.modules names the unit
	Skipped    []string // folders whose own unit shadows it, left untouched

	modulesDir string
	reach      reach
}

// PlanRemove works out the removal of name. It writes nothing.
func PlanRemove(h *Host, hostsDir, machinesDir, name string) (Plan, error) {
	return plan(h, hostsDir, machinesDir, name)
}

// PlanRename works out the rename of oldName to newName. A submodule stays in
// its bundle, so newName is then a bare name; the unit keeps its category and
// its file or folder shape. It writes nothing.
func PlanRename(h *Host, hostsDir, machinesDir, oldName, newName string) (Plan, error) {
	p, err := plan(h, hostsDir, machinesDir, oldName)
	if err != nil {
		return p, err
	}
	p.NewName = newName
	if parent := Parent(p.Unit.Name); parent != "" {
		if strings.Contains(newName, nameSep) {
			return p, errors.New("rename keeps a submodule in its bundle: give only the new name")
		}
		p.NewName = parent + nameSep + newName
	}
	if existing, found := h.Find(p.NewName); found {
		if !CanShadow(p.Unit.Path, existing) {
			return p, fmt.Errorf("module name '%s' already exists at modules/%s", p.NewName, existing.Path)
		}
		p.Shadows = existing.Path
	}
	fi, err := os.Stat(p.Unit.Abs)
	if err != nil {
		return p, err
	}
	base := newName
	if !fi.IsDir() {
		base += nixExt
	}
	p.NewPath = path.Join(path.Dir(p.Unit.Path), base)
	if _, err := os.Lstat(filepath.Join(p.modulesDir, p.NewPath)); err == nil {
		return p, fmt.Errorf("'modules/%s' already exists", p.NewPath)
	} else if !os.IsNotExist(err) {
		return p, err
	}
	return p, nil
}

// plan resolves name, refusing framework modules and the machine's boot unit,
// and collects its reach. Every dependent's luxos.modules use must be the
// recognized shape and every selection in reach recognizable, or nothing is
// planned.
func plan(h *Host, hostsDir, machinesDir, name string) (Plan, error) {
	m, err := h.Lookup(name)
	if err != nil {
		return Plan{}, err
	}
	switch m.Role() {
	case RoleFramework:
		return Plan{}, fmt.Errorf("'%s' is a framework module (modules/%s) — not owned by this config", name, m.Path)
	case RoleBoot:
		return Plan{}, errBootUnit
	}
	r, err := scopeOf(hostsDir, machinesDir, h, m)
	if err != nil {
		return Plan{}, err
	}
	imp, err := importers(r.Selections, m.Name)
	if err != nil {
		return Plan{}, err
	}
	deps, err := dependents(h.ModulesDir, r.UnitDirs, m.Name)
	if err != nil {
		return Plan{}, err
	}
	for _, file := range deps {
		if _, err := nix.Names(file); err != nil {
			return Plan{}, err
		}
	}
	return Plan{Unit: m, Importers: imp, Dependents: deps, Skipped: r.Skipped, modulesDir: h.ModulesDir, reach: r}, nil
}

// Apply carries out the plan in one order: luxos.modules references, selection
// lines, a bundle's submodule lines, the unit's files, then its settings.
// kept lists settings files left in place because the new name already has
// one there.
func (p Plan) Apply() (kept []string, err error) {
	bundle := IsBundle(p.Unit.Abs)
	if err := retargetRefs(p.Dependents, p.Unit.Name, p.NewName); err != nil {
		return nil, err
	}
	if _, _, err := retarget(p.reach.Selections, p.Unit.Name, p.NewPath); err != nil {
		return nil, err
	}
	if bundle {
		if err := retargetPrefix(p.reach.Selections, p.Unit.Path, p.NewPath); err != nil {
			return nil, err
		}
	}
	src := filepath.Join(p.modulesDir, p.Unit.Path)
	if p.NewPath == "" {
		err = os.RemoveAll(src)
	} else {
		err = os.Rename(src, filepath.Join(p.modulesDir, p.NewPath))
	}
	if err != nil {
		return nil, err
	}
	return moveSettings(p.reach.SettingsDirs, p.Unit.Name, p.NewName)
}
