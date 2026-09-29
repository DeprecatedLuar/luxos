package refs

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

const entrypointFile = "modules.nix"

type SelectedUnit struct {
	Path  string
	Root  string
	Rel   string
	Files []string
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
