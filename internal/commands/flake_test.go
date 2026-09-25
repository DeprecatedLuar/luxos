package commands

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/staging"
)

func testLockGraph() staging.LockGraph {
	return staging.LockGraph{
		Root: []string{"ambxst", "flake-file", "luxos", "old", "shared"},
		Nodes: map[string]staging.LockNode{
			staging.LockRootNode: {Inputs: map[string]string{
				"ambxst": "ambxst", "flake-file": "flake-file", "luxos": "luxos", "old": "old", "shared": "shared_2",
			}},
			"ambxst":    {Inputs: map[string]string{"axctl": "axctl", "nixpkgs": "nixpkgs_2"}},
			"axctl":     {Inputs: map[string]string{"deep": "deep"}},
			"deep":      {Inputs: map[string]string{"back": "ambxst"}},
			"nixpkgs_2": {},
			"luxos":     {},
			"old":       {},
			"shared_2":  {},
		},
	}
}

func rowByName(rows []moduleRow, name string) (moduleRow, bool) {
	for _, r := range rows {
		if r.name == name {
			return r, true
		}
	}
	return moduleRow{}, false
}

func TestFlakeBuildRowsMarkersAndPlacement(t *testing.T) {
	decls := map[string][]string{
		"ambxst":     {"desktop/shells/ambxst"},
		"shared":     {"a.nix", "b/c.nix"},
		"newone":     {"local/x.nix"},
		"flake-file": {"y.nix"},
		"nixpkgs":    {"nixpkgs.nix"},
	}
	rows := flakeBuildRows(decls, testLockGraph())

	cases := []struct {
		name, marker string
		category     []string
	}{
		{"ambxst", markerEnabledBoth, []string{"desktop", "shells"}},
		{"shared", markerEnabledBoth, nil},
		{"newone", markerEnabledOnly, []string{"local"}},
		{"old", markerRunningOnly, nil},
		{"luxos", markerEnabledBoth, nil},
		{"nixpkgs", markerEnabledOnly, nil},
	}
	for _, c := range cases {
		r, ok := rowByName(rows, c.name)
		if !ok {
			t.Errorf("row %q missing", c.name)
			continue
		}
		if r.marker != c.marker {
			t.Errorf("%s marker = %q, want %q", c.name, r.marker, c.marker)
		}
		if !reflect.DeepEqual(r.category, c.category) {
			t.Errorf("%s category = %v, want %v", c.name, r.category, c.category)
		}
	}
	if _, ok := rowByName(rows, "flake-file"); ok {
		t.Error("flake-file must never be listed")
	}
	if len(rows) != len(cases) {
		t.Errorf("got %d rows, want %d", len(rows), len(cases))
	}
}

func TestFlakeBuildRowsBuiltinsWithoutDeclarationsOrLock(t *testing.T) {
	rows := flakeBuildRows(nil, staging.LockGraph{})
	for _, name := range []string{"luxos", "nixpkgs"} {
		r, ok := rowByName(rows, name)
		if !ok || r.marker != markerEnabledOnly || len(r.category) != 0 {
			t.Errorf("%s = %+v, want root %s", name, r, markerEnabledOnly)
		}
	}
}

func TestFlakeBuildRowsTransitive(t *testing.T) {
	rows := flakeBuildRows(map[string][]string{"ambxst": {"a.nix"}}, testLockGraph())
	r, _ := rowByName(rows, "ambxst")

	axctl, ok := rowByName(r.children, "axctl")
	if !ok || axctl.marker != markerPulled {
		t.Fatalf("axctl child = %+v", axctl)
	}
	if _, ok := rowByName(r.children, "nixpkgs"); !ok {
		t.Error("nixpkgs child missing")
	}
	deep, ok := rowByName(axctl.children, "deep")
	if !ok {
		t.Fatal("grandchild deep missing (no depth limit)")
	}
	if len(deep.children) != 0 {
		t.Errorf("cycle back to ambxst must be cut, got %+v", deep.children)
	}
}

func TestFlakeRenderTreeAndPlain(t *testing.T) {
	rows := flakeBuildRows(map[string][]string{"ambxst": {"desktop/shells/ambxst"}}, testLockGraph())

	var tty strings.Builder
	moduleRenderTTY(&tty, rows, flakeInputsLabel, treePalette{})
	wantTTY := `inputs/
├── ⊕ nixpkgs
├── ◉ luxos
├── ⊘ old
├── ⊘ shared
└── desktop/
    └── shells/
        └── ◉ ambxst
            ├── ◍ axctl
            │   └── ◍ deep
            └── ◍ nixpkgs

`
	if tty.String() != wantTTY {
		t.Errorf("tree:\n%s\nwant:\n%s", tty.String(), wantTTY)
	}

	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	wantPlain := "ambxst/axctl\nambxst/axctl/deep\nambxst/nixpkgs\ndesktop/shells/ambxst\nluxos\nnixpkgs\nold\nshared\n"
	if plain.String() != wantPlain {
		t.Errorf("plain:\n%q\nwant:\n%q", plain.String(), wantPlain)
	}
}
