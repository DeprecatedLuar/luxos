package links

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureMirror_Create(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMirror(localDir, modulesDir, "host1"); err != nil {
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
	want := filepath.Join(localDir, "host1", "modules.nix")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}

	if _, err := os.Stat(filepath.Join(localDir, "host1", "modules")); err != nil {
		t.Fatalf("host modules dir not created: %v", err)
	}
}

func TestEnsureMirror_Repoint(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)

	if err := EnsureMirror(localDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureMirror host1: %v", err)
	}
	if err := EnsureMirror(localDir, modulesDir, "host2"); err != nil {
		t.Fatalf("EnsureMirror host2: %v", err)
	}

	link := filepath.Join(modulesDir, "default.nix")
	raw, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	resolved := filepath.Join(filepath.Dir(link), raw)
	want := filepath.Join(localDir, "host2", "modules.nix")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureMirror_RefusesRealFile(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)
	if err := os.WriteFile(filepath.Join(modulesDir, "default.nix"), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureMirror(localDir, modulesDir, "host1"); err == nil {
		t.Fatalf("expected error for real file at mirror target")
	}
}

func TestEnsureLocalLink_Create(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	localDir := filepath.Join(root, ".local")
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(filepath.Join(localDir, "host1"), 0755)

	if err := EnsureLocalLink(configDir, localDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalLink: %v", err)
	}

	link := filepath.Join(configDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(localDir, "host1")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureLocalLink_Repoint(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	localDir := filepath.Join(root, ".local")
	os.MkdirAll(configDir, 0755)
	os.MkdirAll(filepath.Join(localDir, "host1"), 0755)
	os.MkdirAll(filepath.Join(localDir, "host2"), 0755)

	if err := EnsureLocalLink(configDir, localDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalLink host1: %v", err)
	}
	if err := EnsureLocalLink(configDir, localDir, "host2"); err != nil {
		t.Fatalf("EnsureLocalLink host2: %v", err)
	}

	link := filepath.Join(configDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(localDir, "host2")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureLocalLink_RefusesRealFile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	localDir := filepath.Join(root, ".local")
	os.MkdirAll(configDir, 0755)
	if err := os.WriteFile(filepath.Join(configDir, "local"), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLocalLink(configDir, localDir, "host1"); err == nil {
		t.Fatalf("expected error for real file at local link target")
	}
}

func TestEnsureLocalModules_Create(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)

	if err := EnsureLocalModules(localDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalModules: %v", err)
	}

	link := filepath.Join(modulesDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(localDir, "host1", "modules")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("target dir not created: %v", err)
	}
}

func TestEnsureLocalModules_Repoint(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)

	if err := EnsureLocalModules(localDir, modulesDir, "host1"); err != nil {
		t.Fatalf("EnsureLocalModules host1: %v", err)
	}
	if err := EnsureLocalModules(localDir, modulesDir, "host2"); err != nil {
		t.Fatalf("EnsureLocalModules host2: %v", err)
	}

	link := filepath.Join(modulesDir, "local")
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	want := filepath.Join(localDir, "host2", "modules")
	if resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}
}

func TestEnsureLocalModules_RefusesRealFile(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(modulesDir, 0755)
	if err := os.WriteFile(filepath.Join(modulesDir, "local"), []byte("real"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureLocalModules(localDir, modulesDir, "host1"); err == nil {
		t.Fatalf("expected error for real file at local modules link target")
	}
}

func TestEnsureLocalModules_RefusesRealDirectory(t *testing.T) {
	root := t.TempDir()
	localDir := filepath.Join(root, ".local")
	modulesDir := filepath.Join(root, "modules")
	os.MkdirAll(filepath.Join(modulesDir, "local"), 0755)

	if err := EnsureLocalModules(localDir, modulesDir, "host1"); err == nil {
		t.Fatalf("expected error for real directory at local modules link target")
	}
}
