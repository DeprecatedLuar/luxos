package modules

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Scope is the reach of a remove or rename of one module.
type Scope struct {
	Host      string   // the one host whose selection is rewritten; "" for every host
	LocalDirs []string // local module dirs whose references are rewritten
	SkipHosts []string // hosts whose own local module shadows m, left untouched
}

// ScopeOf picks the reach of a remove or rename of m. A local module is
// scoped to the active host alone. A shared module reaches every host: its
// selection lines and every host's local module references, except the hosts
// whose local modules shadow it (or, for a submodule, its bundle), which keep
// meaning their own.
func ScopeOf(machinesDir, modulesDir, activeHost string, m Module) (Scope, error) {
	if strings.HasPrefix(m.Path, LocalPrefix) {
		return Scope{Host: activeHost, LocalDirs: []string{filepath.Join(machinesDir, activeHost, localModulesDirName)}}, nil
	}

	hosts, err := Hosts(machinesDir)
	if err != nil {
		return Scope{}, err
	}
	var s Scope
	for _, host := range hosts {
		localDir := filepath.Join(machinesDir, host, localModulesDirName)
		ms, err := Walk(modulesDir, localDir)
		if err != nil {
			return Scope{}, fmt.Errorf("%s: %w", host, err)
		}
		if top, ok := Find(ms, Top(m.Name)); ok && top.Shadows != "" {
			s.SkipHosts = append(s.SkipHosts, host)
			continue
		}
		s.LocalDirs = append(s.LocalDirs, localDir)
	}
	return s, nil
}
