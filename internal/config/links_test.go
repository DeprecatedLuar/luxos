package config

import (
	"os"
	"path/filepath"
	"testing"
)

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func scopeFixture(t *testing.T) (root, modulesDir, hostDir, machineDir string) {
	t.Helper()
	root = t.TempDir()
	hostDir = filepath.Join(root, ".local", "hosts", "h1")
	machineDir = filepath.Join(root, ".local", "machines", "abcd")
	mustMkdirAll(t, filepath.Join(hostDir, "modules"))
	mustMkdirAll(t, filepath.Join(machineDir, "modules"))
	modulesDir = filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)
	return root, modulesDir, hostDir, machineDir
}

func TestEnsureScopeLinks(t *testing.T) {
	root, modulesDir, hostDir, machineDir := scopeFixture(t)

	if err := EnsureScopeLinks(root, modulesDir, hostDir, machineDir); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		filepath.Join(modulesDir, "host"):    "../.local/hosts/h1/modules",
		filepath.Join(modulesDir, "machine"): "../.local/machines/abcd/modules",
		filepath.Join(root, "host"):          ".local/hosts/h1",
		filepath.Join(root, "machine"):       ".local/machines/abcd",
	}
	for link, target := range want {
		got, err := os.Readlink(link)
		if err != nil || got != target {
			t.Errorf("%s -> %q (%v), want %q", link, got, err, target)
		}
	}
	if err := EnsureScopeLinks(root, modulesDir, hostDir, machineDir); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestEnsureScopeLinksRepoint(t *testing.T) {
	root, modulesDir, hostDir, machineDir := scopeFixture(t)
	if err := EnsureScopeLinks(root, modulesDir, hostDir, machineDir); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, ".local", "hosts", "h2")
	mustMkdirAll(t, filepath.Join(other, "modules"))
	if err := EnsureScopeLinks(root, modulesDir, other, machineDir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(filepath.Join(modulesDir, "host")); got != "../.local/hosts/h2/modules" {
		t.Errorf("modules/host -> %q", got)
	}
	if got, _ := os.Readlink(filepath.Join(root, "host")); got != ".local/hosts/h2" {
		t.Errorf("host -> %q", got)
	}
}

func TestEnsureScopeLinksRefusesRealEntry(t *testing.T) {
	for _, rel := range []string{"modules/host", "modules/machine", "host", "machine"} {
		root, modulesDir, hostDir, machineDir := scopeFixture(t)
		mustMkdirAll(t, filepath.Join(root, rel))
		if err := EnsureScopeLinks(root, modulesDir, hostDir, machineDir); err == nil {
			t.Errorf("a real %s must be refused", rel)
		}
	}
}
