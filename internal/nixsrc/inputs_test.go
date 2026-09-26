package nixsrc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestBaseChannel(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		url     string
		line    int
		wantErr string
	}{
		{"valid", "{\n  flake-file.inputs.nixpkgs.url = \"github:NixOS/nixpkgs/nixos-25.11\";\n}\n", "github:NixOS/nixpkgs/nixos-25.11", 2, ""},
		{"missing", "{\n  flake-file.inputs.other.url = \"github:a/b\";\n}\n", "", 0, "missing base channel"},
		{"duplicated", "flake-file.inputs.nixpkgs.url = \"a:b\";\nflake-file.inputs.nixpkgs.url = \"c:d\";\n", "", 0, "2 times"},
		{"non-literal", "flake-file.inputs.nixpkgs.url = someVar;\n", "", 0, "missing base channel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "machine.nix")
			if err := os.WriteFile(file, []byte(tc.src), 0644); err != nil {
				t.Fatal(err)
			}
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
