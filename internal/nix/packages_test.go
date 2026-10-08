package nix

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// testPackagesCfg stands in for nixosConfigurations.<host>: three inputs, a
// NixOS default definition outside config/, and one config file declaring
// packages from two channels, one with no pname or position, and a value
// that is not a package.
const testPackagesCfg = `{
  _module.specialArgs.inputs = {
    self.outPath = "/store/self";
    nixpkgs.outPath = "/store/np";
    unstable.outPath = "/store/un";
  };
  options.environment.systemPackages.definitionsWithLocations = [
    { file = "/store/np/nixos/modules/base.nix";
      value = [ { name = "bash-5.2"; pname = "bash"; version = "5.2"; meta.position = "/store/np/pkgs/bash.nix:1"; } ]; }
    { file = "/store/self/config/modules/desktop/hyprland.nix";
      value = [
        { name = "neovim-0.11.4"; pname = "neovim"; version = "0.11.4"; meta.position = "/store/np/pkgs/neovim.nix:3"; }
        { name = "neovim-0.12.0"; pname = "neovim"; version = "0.12.0"; meta.position = "/store/un/pkgs/neovim.nix:3"; }
        { name = "Ambxst-1.3.4"; meta = { }; }
        "not-a-package"
      ]; }
  ];
}`

func TestPackagesExpr(t *testing.T) {
	skipIfNoNix(t)
	out, err := EvalJSON("("+packagesExpr+") "+testPackagesCfg, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	got, err := decodePackages(out)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []PackageDefs{{
		File: "modules/desktop/hyprland.nix",
		Packages: []Package{
			{Name: "neovim-0.11.4", Pname: "neovim", Version: "0.11.4", Source: "nixpkgs"},
			{Name: "neovim-0.12.0", Pname: "neovim", Version: "0.12.0", Source: "unstable"},
			{Name: "Ambxst-1.3.4"},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packages:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestDecodePackages_RejectsBadJSON(t *testing.T) {
	if _, err := decodePackages([]byte("{")); err == nil {
		t.Fatal("decodePackages: want error for malformed JSON")
	}
}

func TestHosts(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	flake := `{ outputs = _: { nixosConfigurations.alpha = { }; nixosConfigurations.beta = { }; }; }`
	if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte(flake), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Hosts(dir)
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}
}
