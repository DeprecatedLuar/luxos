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
	writeFile(t, filepath.Join(other, ".local/hosts/cathode/modules.nix"), "{ imports = [ ]; }\n")
	ready := filepath.Join(root, "ready")
	writeFile(t, filepath.Join(ready, ".local/hosts/nixos/modules.nix"), "{ imports = [ ]; }\n")

	cases := []struct {
		dir  string
		want State
	}{
		{missing, StateNoConfig},
		{empty, StateNoConfig},
		{other, StateNoHost},
		{ready, StateReady},
	}
	for _, c := range cases {
		got, err := ConfigState(c.dir, filepath.Join(c.dir, ".local/hosts"), "nixos")
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		if got != c.want {
			t.Errorf("ConfigState(%s) = %v, want %v", filepath.Base(c.dir), got, c.want)
		}
	}
}

func TestHosts(t *testing.T) {
	hosts := filepath.Join(t.TempDir(), "hosts")
	writeFile(t, filepath.Join(hosts, "nuremberg/modules.nix"), "{ imports = [ ]; }\n")
	writeFile(t, filepath.Join(hosts, "cathode/modules.nix"), "{ imports = [ ]; }\n")
	writeFile(t, filepath.Join(hosts, "half/host.nix"), "{ }\n")

	got, err := Hosts(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cathode", "nuremberg"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}

	got, err = Hosts(filepath.Join(t.TempDir(), "absent"))
	if err != nil || got != nil {
		t.Errorf("Hosts(absent) = %v, %v; want nil, nil", got, err)
	}
}

func TestResolveHost_NoHostIsErrNoHost(t *testing.T) {
	_, err := ResolveHost(t.TempDir(), "nixos")
	if !errors.Is(err, ErrNoHost) {
		t.Fatalf("err = %v, want ErrNoHost", err)
	}
}
