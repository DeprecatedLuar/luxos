package nix

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadSelectionShape(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	cases := []struct {
		name, src string
		attrs     []string
		imports   int
	}{
		{"imports only", "# c\n{ ... }:\n{\n  imports = [\n    ./a.nix # why\n    ./b\n  ];\n}\n", []string{"imports"}, 2},
		{"extra setting", "{ ... }:\n{\n  imports = [ ];\n  services.x.enable = true;\n}\n", []string{"imports", "services"}, 0},
		{"no header", "{\n  imports = [ ./a.nix ];\n}\n", []string{"imports"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := write(t, filepath.Join(dir, c.name+".nix"), c.src)
			got, err := ReadSelectionShape(f)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Attrs, c.attrs) || got.Imports != c.imports {
				t.Errorf("got %+v, want attrs %v imports %d", got, c.attrs, c.imports)
			}
		})
	}
}

func TestReadSelectionShapeModuleArgument(t *testing.T) {
	skipIfNoNix(t)
	f := write(t, filepath.Join(t.TempDir(), "s.nix"), "{ lib, ... }:\n{\n  imports = lib.optional true ./a.nix;\n}\n")
	if _, err := ReadSelectionShape(f); err == nil {
		t.Fatal("want an error for a selection using a module argument")
	}
}
