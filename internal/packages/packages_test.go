package packages

import (
	"reflect"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

var (
	nvim      = nix.Package{Name: "neovim-0.11.4", Pname: "neovim", Version: "0.11.4", Source: "nixpkgs"}
	nvimNext  = nix.Package{Name: "neovim-0.12.0", Pname: "neovim", Version: "0.12.0", Source: "unstable"}
	tlp       = nix.Package{Name: "tlp-1.0", Pname: "tlp", Version: "1.0", Source: "nixpkgs"}
	tlpBumped = nix.Package{Name: "tlp-1.1", Pname: "tlp", Version: "1.1", Source: "nixpkgs"}
	ambxst    = nix.Package{Name: "Ambxst-1.3.4"}
)

func TestDiff_MergesFilesAndStates(t *testing.T) {
	base := []nix.PackageDefs{
		{File: "modules/laptop/packages.nix", Packages: []nix.Package{nvim, tlp}},
	}
	current := []nix.PackageDefs{
		{File: "modules/desktop/hyprland.nix", Packages: []nix.Package{nvim, ambxst}},
		{File: "modules/laptop/packages.nix", Packages: []nix.Package{nvim, tlpBumped, nvimNext}},
	}

	want := []Package{
		{Name: "Ambxst-1.3.4", Files: []string{"modules/desktop/hyprland.nix"}, State: Staged},
		{Name: "neovim-0.11.4", Pname: "neovim", Version: "0.11.4", Source: "nixpkgs",
			Files: []string{"modules/desktop/hyprland.nix", "modules/laptop/packages.nix"}, State: Active},
		{Name: "neovim-0.12.0", Pname: "neovim", Version: "0.12.0", Source: "unstable",
			Files: []string{"modules/laptop/packages.nix"}, State: Staged},
		{Name: "tlp-1.1", Pname: "tlp", Version: "1.1", Source: "nixpkgs",
			Files: []string{"modules/laptop/packages.nix"}, State: Staged},
		{Name: "tlp-1.0", Pname: "tlp", Version: "1.0", Source: "nixpkgs",
			Files: []string{"modules/laptop/packages.nix"}, State: Leftover},
	}
	if got := Diff(base, current); !reflect.DeepEqual(got, want) {
		t.Errorf("Diff:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestDiff_NoBaselineIsAllStaged(t *testing.T) {
	current := []nix.PackageDefs{{File: "machine.nix", Packages: []nix.Package{nvim, tlp}}}
	for _, p := range Diff(nil, current) {
		if p.State != Staged {
			t.Errorf("%s: state %d, want Staged", p.Name, p.State)
		}
	}
}

func TestDiff_SameFileListedOnce(t *testing.T) {
	current := []nix.PackageDefs{
		{File: "a.nix", Packages: []nix.Package{nvim}},
		{File: "a.nix", Packages: []nix.Package{nvim}},
	}
	got := Diff(nil, current)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Files, []string{"a.nix"}) {
		t.Errorf("Diff = %+v, want one package declared in a.nix once", got)
	}
}

func TestNamed(t *testing.T) {
	pkgs := Diff(nil, []nix.PackageDefs{{File: "a.nix", Packages: []nix.Package{nvim, nvimNext, ambxst}}})

	if got := Named(pkgs, "neovim"); len(got) != 2 {
		t.Errorf("Named(neovim) = %d packages, want 2", len(got))
	}
	if got := Named(pkgs, "Ambxst-1.3.4"); len(got) != 1 || got[0].Label() != "Ambxst-1.3.4" {
		t.Errorf("Named(Ambxst-1.3.4) = %+v, want the package without pname", got)
	}
	if got := Named(pkgs, "neovim-0.11.4"); len(got) != 0 {
		t.Errorf("Named(neovim-0.11.4) = %+v, want none: a package with a pname matches only it", got)
	}
}
