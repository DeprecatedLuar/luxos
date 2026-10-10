package templates

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// outputsFixtureConfiguration stands in for the generated
// framework/configuration.nix: it imports through modulesPath the way
// nixos-generate-config's hardware-configuration.nix does, and reports the
// specialArgs it received.
const outputsFixtureConfiguration = `{ lib, modulesPath, hostName, ... }: {
  imports = [ (modulesPath + "/installer/scan/not-detected.nix") ];
  options.fixture = lib.mkOption { type = lib.types.raw; };
  config.fixture = { inherit modulesPath hostName; };
}`

// outputsFixtureExpr calls outputs.nix with a stub nixpkgs input: the local
// nixpkgs lib extended with the flake's nixosSystem.
const outputsFixtureExpr = `
let
  lib = (import <nixpkgs/lib>) // {
    nixosSystem = args: import <nixpkgs/nixos/lib/eval-config.nix> (args // { system = null; });
  };
  nixpkgs = { outPath = toString <nixpkgs>; inherit lib; };
  out = import ./framework/outputs.nix { inherit nixpkgs; } "box";
in {
  hosts = builtins.attrNames out.nixosConfigurations;
  inherit (out.nixosConfigurations.box.config.fixture) hostName modulesPath;
}
`

func TestOutputsProvidesModulesPath(t *testing.T) {
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}

	dir := t.TempDir()
	files := map[string][]byte{
		"framework/configuration.nix": []byte(outputsFixtureConfiguration),
	}
	for _, name := range []string{"outputs.nix", "units.nix", "overlay.nix"} {
		content, err := File("framework/" + name)
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

	var got struct {
		Hosts       []string `json:"hosts"`
		HostName    string   `json:"hostName"`
		ModulesPath string   `json:"modulesPath"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("parse %q: %v", stdout.String(), err)
	}
	if len(got.Hosts) != 1 || got.Hosts[0] != "box" || got.HostName != "box" {
		t.Errorf("hosts = %v, hostName = %q; want exactly box", got.Hosts, got.HostName)
	}
	if !strings.HasSuffix(got.ModulesPath, "/nixos/modules") {
		t.Errorf("modulesPath = %s, want a path ending in /nixos/modules", got.ModulesPath)
	}
}
