package nixsrc

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInputDecls(t *testing.T) {
	src := `{ ... }:
{
  # flake-file.inputs.old.url = "github:a/old";
  flake-file.inputs.unstable.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  flake-file.inputs.my-in_put'.url   =  "github:a/b" ; # trailing
  flake-file.inputs.follows.inputs.nixpkgs.follows = "nixpkgs";
}
`
	file := filepath.Join(t.TempDir(), "m.nix")
	if err := os.WriteFile(file, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := InputDecls(file)
	if err != nil {
		t.Fatal(err)
	}
	want := []InputDecl{
		{Name: "unstable", URL: "github:NixOS/nixpkgs/nixpkgs-unstable", Line: 4},
		{Name: "my-in_put'", URL: "github:a/b", Line: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("InputDecls = %+v, want %+v", got, want)
	}
}

func TestInputDeclsMissingFile(t *testing.T) {
	if _, err := InputDecls(filepath.Join(t.TempDir(), "nope.nix")); err == nil {
		t.Error("want error for a missing file")
	}
}
