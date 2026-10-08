package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/packages"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

func testPackages() []packages.Package {
	return []packages.Package{
		{Name: "Ambxst-1.3.4", Files: []string{"modules/desktop/ambxst.nix"}, State: packages.Staged},
		{Name: "neovim-0.11.4", Pname: "neovim", Version: "0.11.4", Source: "nixpkgs",
			Files: []string{"modules/desktop/hyprland.nix", "modules/laptop/packages.nix"}, State: packages.Active},
		{Name: "neovim-0.12.0", Pname: "neovim", Version: "0.12.0", Source: "unstable",
			Files: []string{"modules/vimsanity/module.nix"}, State: packages.Active},
		{Name: "tlp-1.0", Pname: "tlp", Version: "1.0", Source: "nixpkgs",
			Files: []string{"modules/laptop/packages.nix"}, State: packages.Leftover},
	}
}

func TestPackageRenderTreeAndPlain(t *testing.T) {
	var tree strings.Builder
	packageRenderTree(&tree, testPackages(), ui.Palette{})
	wantTree := `packages/
└── modules/
    ├── desktop/
    │   ├── ambxst.nix
    │   │   └── ⊕ Ambxst-1.3.4
    │   └── hyprland.nix
    │       └── ◉ neovim
    ├── laptop/
    │   └── packages.nix
    │       ├── ◉ neovim
    │       └── ⊘ tlp
    └── vimsanity/
        └── module.nix
            └── ◉ neovim ❄

`
	if tree.String() != wantTree {
		t.Errorf("tree:\n%s\nwant:\n%s", tree.String(), wantTree)
	}

	var plain strings.Builder
	packageRenderPlain(&plain, testPackages())
	wantPlain := "modules/desktop/ambxst.nix\tAmbxst-1.3.4\tstaged\tunknown\tunknown\n" +
		"modules/desktop/hyprland.nix\tneovim\tactive\tnixpkgs\t0.11.4\n" +
		"modules/laptop/packages.nix\tneovim\tactive\tnixpkgs\t0.11.4\n" +
		"modules/laptop/packages.nix\ttlp\tleftover\tnixpkgs\t1.0\n" +
		"modules/vimsanity/module.nix\tneovim\tactive\tunstable\t0.12.0\n"
	if plain.String() != wantPlain {
		t.Errorf("plain:\n%q\nwant:\n%q", plain.String(), wantPlain)
	}
}

func TestPackageRenderEmpty(t *testing.T) {
	var tree, flat strings.Builder
	packageRenderTree(&tree, nil, ui.Palette{})
	packageRenderFlat(&flat, nil, ui.Palette{})
	if tree.String() != packagesEmpty || flat.String() != packagesEmpty {
		t.Errorf("empty renders %q and %q, want %q", tree.String(), flat.String(), packagesEmpty)
	}
}

func TestPackageRenderFlat(t *testing.T) {
	var flat strings.Builder
	packageRenderFlat(&flat, testPackages(), ui.Palette{})
	for _, line := range []string{
		"⊕ Ambxst-1.3.4  ← modules/desktop/ambxst.nix\n",
		"◉ neovim  ← modules/desktop/hyprland.nix, modules/laptop/packages.nix\n",
		"◉ neovim ❄  ← modules/vimsanity/module.nix\n",
		"⊘ tlp  ← modules/laptop/packages.nix\n",
	} {
		if !strings.Contains(flat.String(), line) {
			t.Errorf("flat output lacks %q:\n%s", line, flat.String())
		}
	}
	if n := strings.Count(flat.String(), "\n"); n != 4 {
		t.Errorf("flat output has %d lines, want 4 (one per package)", n)
	}
}

func TestPackageRenderListJSON(t *testing.T) {
	var out strings.Builder
	if err := packageRenderListJSON(&out, testPackages()); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(got) != 5 {
		t.Fatalf("got %d entries, want 5 (one per file and package)", len(got))
	}
	first := got[0]
	if first["file"] != "modules/desktop/ambxst.nix" || first["name"] != "Ambxst-1.3.4" || first["state"] != "staged" {
		t.Errorf("first entry = %v", first)
	}
	if first["source"] != nil || first["version"] != nil {
		t.Errorf("unknown source and version must be null, got %v", first)
	}

	var empty strings.Builder
	if err := packageRenderListJSON(&empty, nil); err != nil {
		t.Fatal(err)
	}
	if empty.String() != "[]\n" {
		t.Errorf("empty JSON = %q, want []", empty.String())
	}
}

func TestPackageBaseline(t *testing.T) {
	root := t.TempDir()
	mkdir := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	modulesDir := filepath.Join(root, "modules")
	mkdir(filepath.Join(root, ".local/machines/alpha/modules"))
	mkdir(modulesDir)
	if err := os.Symlink("../.local/machines/alpha/modules", filepath.Join(modulesDir, "local")); err != nil {
		t.Fatal(err)
	}
	stagingDir := filepath.Join(root, "etc-nixos")
	p := paths.Paths{Modules: modulesDir, Staging: stagingDir}

	check := func(host, want string) {
		t.Helper()
		got, err := packageBaseline(p, host)
		if err != nil {
			t.Fatalf("packageBaseline(%s): %v", host, err)
		}
		if got != want {
			t.Errorf("packageBaseline(%s) = %q, want %q", host, got, want)
		}
	}

	check("alpha", "") // no luxos stage yet
	mkdir(filepath.Join(stagingDir, "config", "modules"))
	check("alpha", stagingDir)
	check("beta", "") // the stage holds only the active host
}
