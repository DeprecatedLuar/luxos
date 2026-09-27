package nixsrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeNixFile(t *testing.T, dir, name, src string) string {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestInputDeclsFlatForm(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := writeNixFile(t, dir, "m.nix", `{ ... }:
{
  # flake-file.inputs.old.url = "github:a/old";
  flake-file.inputs.unstable.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
}
`)

	got, err := InputDecls(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("InputDecls = %+v, want 1 decl", got)
	}
	d := got[0]
	if d.Name != "unstable" || d.URL != "github:NixOS/nixpkgs/nixpkgs-unstable" || d.Line != 4 {
		t.Errorf("InputDecls = %+v", d)
	}
	if len(d.Value) != 1 || d.Value["url"] != d.URL {
		t.Errorf("Value = %+v, want just {url: %q}", d.Value, d.URL)
	}
}

func TestInputDeclsNestedFormWithFollows(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := writeNixFile(t, dir, "m.nix", `{ inputs, ... }:
{
  flake-file.inputs.nixos-hardware = {
    url = "github:NixOS/nixos-hardware";
    inputs.nixpkgs.follows = "nixpkgs";
  };

  imports = [ inputs.nixos-hardware.nixosModules.common-gpu-intel ];
}
`)

	got, err := InputDecls(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("InputDecls = %+v, want 1 decl", got)
	}
	d := got[0]
	if d.Name != "nixos-hardware" || d.URL != "github:NixOS/nixos-hardware" || d.Line != 3 {
		t.Errorf("InputDecls = %+v", d)
	}
	nested, ok := d.Value["inputs"].(map[string]any)
	if !ok {
		t.Fatalf("Value[inputs] = %#v, want a map", d.Value["inputs"])
	}
	np, ok := nested["nixpkgs"].(map[string]any)
	if !ok || np["follows"] != "nixpkgs" {
		t.Errorf("Value[inputs][nixpkgs] = %#v, want follows=nixpkgs", nested["nixpkgs"])
	}
}

func TestInputDeclsBatchAcrossFiles(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f1 := writeNixFile(t, dir, "a.nix", `{ }: { flake-file.inputs.a.url = "github:x/a"; }`)
	f2 := writeNixFile(t, dir, "b.nix", `{ }: { flake-file.inputs.b.url = "github:x/b"; }`)

	got, err := InputDecls(f1, f2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].File != f1 || got[0].Name != "a" || got[1].File != f2 || got[1].Name != "b" {
		t.Errorf("InputDecls = %+v, want a then b, in file order", got)
	}
}

// TestInputDeclsStubDependentFile covers a file that needs a real function
// argument (a callPackage-style file, say): it must yield no declarations
// rather than failing the whole batch.
func TestInputDeclsStubDependentFile(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := writeNixFile(t, dir, "package.nix", `{ pkgs }:
{
  flake-file.inputs.dep.url = pkgs.lib.toUpper "bad";
}
`)

	got, err := InputDecls(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("InputDecls = %+v, want none (stub pkgs should have thrown)", got)
	}
}

func TestInputDeclsMissingFile(t *testing.T) {
	skipIfNoNix(t)
	if _, err := InputDecls(filepath.Join(t.TempDir(), "nope.nix")); err == nil {
		t.Error("want error for a missing file")
	}
}

func TestBaseChannel(t *testing.T) {
	skipIfNoNix(t)
	cases := []struct {
		name    string
		src     string
		url     string
		line    int
		wantErr string
	}{
		{"valid", `{ flake-file.inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11"; }`, "github:NixOS/nixpkgs/nixos-25.11", 1, ""},
		{"missing", `{ flake-file.inputs.other.url = "github:a/b"; }`, "", 0, "missing base channel"},
		{"computed", `let mine = "github:computed/url"; in { flake-file.inputs.nixpkgs.url = mine; }`, "github:computed/url", 1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := writeNixFile(t, t.TempDir(), "machine.nix", tc.src)
			url, line, err := BaseChannel(file)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || url != tc.url || line != tc.line {
				t.Fatalf("got (%q, %d, %v), want (%q, %d)", url, line, err, tc.url, tc.line)
			}
		})
	}
}

func TestRenderInputs(t *testing.T) {
	skipIfNoNix(t)
	decls := []InputDecl{
		{Name: "nixpkgs", Value: map[string]any{"url": "github:NixOS/nixpkgs/nixos-25.11"}},
		{Name: "nixos-hardware", Value: map[string]any{
			"url":    "github:NixOS/nixos-hardware",
			"inputs": map[string]any{"nixpkgs": map[string]any{"follows": "nixpkgs"}},
		}},
		{Name: "nixos-hardware", Value: map[string]any{"url": "should-be-ignored"}}, // repeat: first wins
	}
	got := RenderInputs(decls)

	// RenderInputs renders the body of flake.nix's `inputs = { ... };` block:
	// wrap it in a synthetic flake-file.inputs to read it back with
	// InputDecls, exactly as flake.nix's own shape would if it also declared
	// itself that way.
	dir := t.TempDir()
	file := writeNixFile(t, dir, "rendered.nix", "{ flake-file.inputs = {\n"+got+"}; }\n")

	back, err := InputDecls(file)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]InputDecl{}
	for _, d := range back {
		byName[d.Name] = d
	}
	if byName["nixpkgs"].URL != "github:NixOS/nixpkgs/nixos-25.11" {
		t.Errorf("nixpkgs round-trip = %+v", byName["nixpkgs"])
	}
	hw := byName["nixos-hardware"]
	if hw.URL != "github:NixOS/nixos-hardware" {
		t.Errorf("nixos-hardware did not keep the first declaration: %+v", hw)
	}
	nested, _ := hw.Value["inputs"].(map[string]any)
	np, _ := nested["nixpkgs"].(map[string]any)
	if np["follows"] != "nixpkgs" {
		t.Errorf("nixos-hardware lost its follows: %+v", hw.Value)
	}
}
