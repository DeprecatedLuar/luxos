package refs

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

// entrypointFile is the host's selection file, read the same way
// internal/heal and internal/commands do.
const entrypointFile = "modules.nix"

// SelectedUnit is one unit reachable for host: the host's own selection plus
// everything its selected units pull in through luxos.modules.
type SelectedUnit struct {
	Path  string   // unit path as units.Resolve returns it ("local/" prefix for a local unit)
	Root  string   // filesystem root Files sit under: modulesDir, or localModulesDir for a local unit
	Rel   string   // Path relative to Root
	Files []string // absolute *.nix files belonging to this unit
}

// SelectedUnits returns, for host's entrypoint at hostDir, every unit its
// selection resolves to (units.Resolve) plus every unit pulled in through
// luxos.modules (Closure), each with its files. A selected or pulled name
// that resolves to nothing is silently skipped, the same as Closure: a
// broken selection is already caught by Validate at rebuild time.
func SelectedUnits(modulesDir, localModulesDir, hostDir string) ([]SelectedUnit, error) {
	us, err := units.Walk(modulesDir, localModulesDir)
	if err != nil {
		return nil, err
	}
	selection, err := imports.List(filepath.Join(hostDir, entrypointFile))
	if err != nil {
		return nil, err
	}
	pulled, err := Closure(modulesDir, us, selection)
	if err != nil {
		return nil, err
	}

	names := map[string]bool{}
	for _, sel := range selection {
		names[units.NameFromPath(sel)] = true
	}
	for name := range pulled {
		names[name] = true
	}

	var out []SelectedUnit
	for name := range names {
		unitPath, ok := units.Resolve(us, name)
		if !ok {
			continue
		}
		root, rel := modulesDir, unitPath
		if trimmed, ok := strings.CutPrefix(unitPath, localPrefix); ok {
			root, rel = localModulesDir, trimmed
		}
		files, err := UnitFiles(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		out = append(out, SelectedUnit{Path: unitPath, Root: root, Rel: rel, Files: files})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
