package framework

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// outputsFixtureFlakeFile stands in for the generated framework/flake-file.nix:
// it imports through modulesPath the way nixos-generate-config's
// hardware-configuration.nix does, and reports the value it received.
const outputsFixtureFlakeFile = `{ modulesPath, ... }: {
  imports = [ (modulesPath + "/installer/scan/not-detected.nix") ];
  outputs = _: modulesPath;
}`

// outputsFixtureExpr calls outputs.nix with stub inputs: the local nixpkgs,
// and a flake-file module declaring only the options the fixture sets.
const outputsFixtureExpr = `
let
  nixpkgs = { outPath = <nixpkgs>; lib = import <nixpkgs/lib>; };
  flakeModule = { lib, ... }: {
    options.outputs = lib.mkOption { type = lib.types.raw; };
    options.hardware.enableRedistributableFirmware = lib.mkOption { type = lib.types.bool; };
  };
in import ./framework/outputs.nix {
  inherit nixpkgs;
  self = { };
  flake-file.flakeModules.flake = flakeModule;
}
`

func TestOutputsProvidesModulesPath(t *testing.T) {
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}

	dir := t.TempDir()
	files := map[string][]byte{
		"framework/flake-file.nix": []byte(outputsFixtureFlakeFile),
	}
	for _, name := range []string{"outputs.nix", "units.nix"} {
		content, err := File(name)
		if err != nil {
			t.Fatal(err)
		}
		files["framework/"+name] = content
	}
	for rel, content := range files {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "config", "modules"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", outputsFixtureExpr)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("eval failed: %v\n%s", err, stderr.String())
	}

	want := "/nixos/modules\""
	if got := strings.TrimSpace(stdout.String()); !strings.HasSuffix(got, want) {
		t.Fatalf("modulesPath = %s, want a path ending in %s", got, want)
	}
}
