// Package packages compares the packages two evaluations of a host's config
// declare. A package's identity is its source plus its full name.
package packages

import (
	"slices"
	"sort"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// State is where a package stands between the running system and the config.
type State int

const (
	Active   State = iota // in both
	Staged                // only in the config: the next switch adds it
	Leftover              // only in the running system: the next switch removes it
)

// Package is one package with the config files declaring it.
type Package struct {
	Name, Pname, Version, Source string
	Files                        []string // relative to the stage's config/, in definition order
	State                        State
}

// Label is the name a package is shown and looked up by: its pname, or its
// full name when it has none.
func (p Package) Label() string {
	if p.Pname != "" {
		return p.Pname
	}
	return p.Name
}

type identity struct{ source, name string }

// Diff merges each evaluation's definitions into packages and states them.
// base is the running system's (nil when there is none: every package is
// Staged), current the config's. Sorted by label, then source.
func Diff(base, current []nix.PackageDefs) []Package {
	cur, curOrder := collect(current)
	old, oldOrder := collect(base)

	var out []Package
	for _, id := range curOrder {
		p := *cur[id]
		p.State = Staged
		if _, ok := old[id]; ok {
			p.State = Active
		}
		out = append(out, p)
	}
	for _, id := range oldOrder {
		if _, ok := cur[id]; ok {
			continue
		}
		p := *old[id]
		p.State = Leftover
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Label() != out[j].Label() {
			return out[i].Label() < out[j].Label()
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// collect merges defs by identity, in first-seen order, each file once.
func collect(defs []nix.PackageDefs) (map[identity]*Package, []identity) {
	byID := map[identity]*Package{}
	var order []identity
	for _, d := range defs {
		for _, np := range d.Packages {
			id := identity{np.Source, np.Name}
			p, ok := byID[id]
			if !ok {
				p = &Package{Name: np.Name, Pname: np.Pname, Version: np.Version, Source: np.Source}
				byID[id] = p
				order = append(order, id)
			}
			if !slices.Contains(p.Files, d.File) {
				p.Files = append(p.Files, d.File)
			}
		}
	}
	return byID, order
}

// Named returns the packages labelled name.
func Named(pkgs []Package, name string) []Package {
	var out []Package
	for _, p := range pkgs {
		if p.Label() == name {
			out = append(out, p)
		}
	}
	return out
}
