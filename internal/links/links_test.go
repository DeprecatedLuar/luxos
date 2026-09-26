package links

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureMirror_Create(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMirror(machinesDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}

	link := filepath.Join(modulesDir, "default.nix")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", link)
	}

	raw, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	resolved := filepath.Join(filepath.Dir(link), raw)
	want := filepath.Join(machinesDir, "host1", "modules.nix")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}

	if _, err := os.Stat(filepath.Join(machinesDir, "host1", "modules")); err != nil {
		t.Fatalf("host modules dir not created: %v", err)
	}
}

func TestEnsureMirror_Repoint(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)

	if err := EnsureMirror(machinesDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureMirror host1: %v", err)
	}
	if err := EnsureMirror(machinesDir, modulesDir, "host2"); err != nil {
		t.Fatalf("EnsureMirror host2: %v", err)
	}

	link := filepath.Join(modulesDir, "default.nix")
	raw, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	resolved := filepath.Join(filepath.Dir(link), raw)
	want := filepath.Join(machinesDir, "host2", "modules.nix")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureMirror_RefusesRealFile(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)
	if err := os.WriteFile(filepath.Join(modulesDir, "default.nix"), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMirror(machinesDir, modulesDir, "host1"); err == nil {
		t.Fatalf("expected error for real file at mirror target")
	}
}

func TestEnsureLocalLink_Create(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	machinesDir := filepath.Join(root, ".local", "machines")
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(filepath.Join(machinesDir, "host1"), 0755)

	if err := EnsureLocalLink(configDir, machinesDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalLink: %v", err)
	}

	link := filepath.Join(configDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(machinesDir, "host1")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureLocalLink_Repoint(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	machinesDir := filepath.Join(root, ".local", "machines")
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(filepath.Join(machinesDir, "host1"), 0755)
	os.MkdirAll(filepath.Join(machinesDir, "host2"), 0755)

	if err := EnsureLocalLink(configDir, machinesDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalLink host1: %v", err)
	}
	if err := EnsureLocalLink(configDir, machinesDir, "host2"); err != nil {
		t.Fatalf("EnsureLocalLink host2: %v", err)
	}

	link := filepath.Join(configDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(machinesDir, "host2")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureLocalLink_RefusesRealFile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	machinesDir := filepath.Join(root, ".local", "machines")
	os.MkdirAll(configDir, 0755)
	if err := os.WriteFile(filepath.Join(configDir, "local"), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLocalLink(configDir, machinesDir, "host1"); err == nil {
		t.Fatalf("expected error for real file at local link target")
	}
}

func TestEnsureLocalModules_Create(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)

	if err := EnsureLocalModules(machinesDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalModules: %v", err)
	}

	link := filepath.Join(modulesDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(machinesDir, "host1", "modules")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("target dir not created: %v", err)
	}
}

func TestEnsureLocalModules_Repoint(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)

	if err := EnsureLocalModules(machinesDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalModules host1: %v", err)
	}
	if err := EnsureLocalModules(machinesDir, modulesDir, "host2"); err != nil {
		t.Fatalf("EnsureLocalModules host2: %v", err)
	}

	link := filepath.Join(modulesDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(machinesDir, "host2", "modules")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureLocalModules_RefusesRealFile(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)
	if err := os.WriteFile(filepath.Join(modulesDir, "local"), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLocalModules(machinesDir, modulesDir, "host1"); err == nil {
		t.Fatalf("expected error for real file at local modules link target")
	}
}

func TestEnsureLocalModules_RefusesRealDirectory(t *testing.T) {
	root := t.TempDir()
	machinesDir := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(filepath.Join(modulesDir, "local"), 0755)

	if err := EnsureLocalModules(machinesDir, modulesDir, "host1"); err == nil {
		t.Fatalf("expected error for real directory at local modules link target")
	}
}

func TestEnsureHardwareLink(t *testing.T) {
	root := t.TempDir()
	machines := filepath.Join(root, "machines")
	hardware := filepath.Join(root, "hardware")
	for _, h := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(machines, h, "modules"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// A machine without a modules/ directory is skipped.
	if err := os.MkdirAll(filepath.Join(machines, "c"), 0755); err != nil {
		t.Fatal(err)
	}
	// A stale link in the active host and a link in the other host.
	for _, h := range []string{"a", "b"} {
		if err := os.Symlink("../../../hardware/old", filepath.Join(machines, h, "modules", "hardware-support")); err != nil {
			t.Fatal(err)
		}
	}

	if err := EnsureHardwareLink(machines, hardware, "a", "k1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(filepath.Join(machines, "a", "modules", "hardware-support")); got != "../../../hardware/k1" {
		t.Errorf("link target = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(machines, "b", "modules", "hardware-support")); !os.IsNotExist(err) {
		t.Errorf("other host's link not removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(machines, "c", "modules")); !os.IsNotExist(err) {
		t.Errorf("skipped machine gained a modules dir: %v", err)
	}
	if err := EnsureHardwareLink(machines, hardware, "a", "k1"); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestEnsureHardwareLink_RefusesRealDirectory(t *testing.T) {
	root := t.TempDir()
	machines := filepath.Join(root, "machines")
	if err := os.MkdirAll(filepath.Join(machines, "a"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(machines, "b", "modules", "hardware-support"), 0755); err != nil {
		t.Fatal(err)
	}
	err := EnsureHardwareLink(machines, filepath.Join(root, "hardware"), "a", "k1")
	if err == nil || !strings.Contains(err.Error(), "real file/dir") {
		t.Fatalf("err = %v", err)
	}
}
