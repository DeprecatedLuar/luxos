package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigState(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "other")
	writeFile(t, filepath.Join(other, ".local/machines/cathode/modules.nix"), "{ imports = [ ]; }\n")
	ready := filepath.Join(root, "ready")
	writeFile(t, filepath.Join(ready, ".local/machines/nixos/modules.nix"), "{ imports = [ ]; }\n")

	cases := []struct {
		dir  string
		want State
	}{
		{missing, StateNoConfig},
		{empty, StateNoConfig},
		{other, StateNoMachine},
		{ready, StateReady},
	}
	for _, c := range cases {
		got, err := ConfigState(c.dir, filepath.Join(c.dir, ".local/machines"), "nixos")
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		if got != c.want {
			t.Errorf("ConfigState(%s) = %v, want %v", filepath.Base(c.dir), got, c.want)
		}
	}
}

func TestMachines(t *testing.T) {
	machines := filepath.Join(t.TempDir(), "machines")
	writeFile(t, filepath.Join(machines, "nuremberg/modules.nix"), "{ imports = [ ]; }\n")
	writeFile(t, filepath.Join(machines, "cathode/modules.nix"), "{ imports = [ ]; }\n")
	writeFile(t, filepath.Join(machines, "half/machine.nix"), "{ }\n")

	got, err := Machines(machines)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cathode", "nuremberg"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Machines = %v, want %v", got, want)
	}

	got, err = Machines(filepath.Join(t.TempDir(), "absent"))
	if err != nil || got != nil {
		t.Errorf("Machines(absent) = %v, %v; want nil, nil", got, err)
	}
}

func TestResolveHost_NoMachineIsErrNoMachine(t *testing.T) {
	_, err := ResolveHost(t.TempDir(), "nixos")
	if !errors.Is(err, ErrNoMachine) {
		t.Fatalf("err = %v, want ErrNoMachine", err)
	}
}
