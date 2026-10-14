package modules

import (
	"fmt"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

// Scope is the reach of a remove or rename of one module.
type Scope struct {
	Selections   []SelectionFile // selections whose lines are rewritten
	UnitDirs     []string        // scope modules folders whose luxos.modules references are rewritten
	SettingsDirs []string        // host or machine folders whose settings follow the unit
	Skipped      []string        // folders whose own unit shadows m, left untouched
}

// ScopeOf picks the reach of a remove or rename of m. A host unit reaches
// the active host; a machine unit, this machine. A shared unit reaches every
// host and machine folder except those holding their own unit of that name
// (its bundle's, for a submodule), which keep meaning their own.
func ScopeOf(hostsDir, machinesDir string, h *Host, m Module) (Scope, error) {
	own := func(dir, scope string) Scope {
		return Scope{
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

	var s Scope
	for _, root := range []struct{ scope, dir string }{{config.ScopeHost, hostsDir}, {config.ScopeMachine, machinesDir}} {
		names, err := scopeFolders(root.scope, root.dir)
		if err != nil {
			return Scope{}, err
		}
		for _, n := range names {
			folder := filepath.Join(root.dir, n)
			units, err := walkScope(filepath.Join(folder, config.ScopeModulesDir), root.scope)
			if err != nil {
				return Scope{}, fmt.Errorf("%s: %w", folder, err)
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
	return s, nil
}
