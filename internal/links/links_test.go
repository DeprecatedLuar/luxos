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
	want := filepath.Join(localDir, "host1", "modules", "default.nix")
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
	want := filepath.Join(localDir, "host2", "modules", "default.nix")
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

func TestEnsureEtcNixos_SymlinkedAndMissing(t *testing.T) {
	root := t.TempDir()

	// Case 1: etcNixos is a symlink to a real directory elsewhere.
	realDir := filepath.Join(root, "real-etc")
	os.MkdirAll(realDir, 0755)
	etcNixos := filepath.Join(root, "etc-nixos")
	if err := os.Symlink(realDir, etcNixos); err != nil {
		t.Fatal(err)
	}
	// Leave a stale configuration.nix symlink behind in the real dir.
	staleTarget := filepath.Join(root, "stale-target.nix")
	os.WriteFile(staleTarget, []byte("x"), 0644)
	if err := os.Symlink(staleTarget, filepath.Join(realDir, "configuration.nix")); err != nil {
		t.Fatal(err)
	}

	actions, err := EnsureEtcNixos(etcNixos)
	if err != nil {
		t.Fatalf("EnsureEtcNixos: %v", err)
	}
	if len(actions) == 0 {
		t.Fatalf("expected actions, got none")
	}

	info, err := os.Lstat(etcNixos)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s is still a symlink", etcNixos)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", etcNixos)
	}

	if _, err := os.Lstat(filepath.Join(etcNixos, "configuration.nix")); !os.IsNotExist(err) {
		t.Fatalf("stale configuration.nix symlink not removed, err=%v", err)
	}

	envInfo, err := os.Stat(filepath.Join(etcNixos, "env"))
	if err != nil {
		t.Fatalf("env file not created: %v", err)
	}
	if envInfo.Mode().Perm() != 0600 {
		t.Fatalf("env file mode = %o, want 0600", envInfo.Mode().Perm())
	}

	// Case 2: missing directory entirely.
	missing := filepath.Join(root, "missing-etc")
	actions2, err := EnsureEtcNixos(missing)
	if err != nil {
		t.Fatalf("EnsureEtcNixos (missing): %v", err)
	}
	if len(actions2) == 0 {
		t.Fatalf("expected actions for missing dir")
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Fatalf("missing dir not created: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(missing, "env")); err != nil {
		t.Fatalf("env file not created in missing dir: %v", err)
	}
}

func TestEnsureEtcNixos_Idempotent(t *testing.T) {
	etcNixos := filepath.Join(t.TempDir(), "etc-nixos")

	if _, err := EnsureEtcNixos(etcNixos); err != nil {
		t.Fatalf("first EnsureEtcNixos: %v", err)
	}
	actions, err := EnsureEtcNixos(etcNixos)
	if err != nil {
		t.Fatalf("second EnsureEtcNixos: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("expected no actions on second run, got %v", actions)
	}
}

func TestEnsureLux_Create(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "bin", "lux")
	target := filepath.Join(root, "luxos")

	state, err := EnsureLux(link, target)
	if err != nil {
		t.Fatalf("EnsureLux: %v", err)
	}
	if state != LuxLinkCreated {
		t.Fatalf("state = %v, want LuxLinkCreated", state)
	}

	resolved, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if resolved != target {
		t.Fatalf("link = %s, want %s", resolved, target)
	}
}

func TestEnsureLux_OK(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "bin", "lux")
	target := filepath.Join(root, "luxos")

	if _, err := EnsureLux(link, target); err != nil {
		t.Fatalf("first EnsureLux: %v", err)
	}
	state, err := EnsureLux(link, target)
	if err != nil {
		t.Fatalf("second EnsureLux: %v", err)
	}
	if state != LuxLinkOK {
		t.Fatalf("state = %v, want LuxLinkOK", state)
	}
}

func TestEnsureLux_ConflictRealFile(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	os.MkdirAll(binDir, 0755)
	link := filepath.Join(binDir, "lux")
	target := filepath.Join(root, "luxos")

	if err := os.WriteFile(link, []byte("real"), 0755); err != nil {
		t.Fatal(err)
	}

	state, err := EnsureLux(link, target)
	if err != nil {
		t.Fatalf("EnsureLux: %v", err)
	}
	if state != LuxLinkConflict {
		t.Fatalf("state = %v, want LuxLinkConflict", state)
	}

	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "real" {
		t.Fatalf("real file was modified: %q", data)
	}
}

func TestEnsureLux_ConflictSymlinkElsewhere(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	os.MkdirAll(binDir, 0755)
	link := filepath.Join(binDir, "lux")
	target := filepath.Join(root, "luxos")
	other := filepath.Join(root, "elsewhere")

	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}

	state, err := EnsureLux(link, target)
	if err != nil {
		t.Fatalf("EnsureLux: %v", err)
	}
	if state != LuxLinkConflict {
		t.Fatalf("state = %v, want LuxLinkConflict", state)
	}

	resolved, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if resolved != other {
		t.Fatalf("symlink was changed: %s, want untouched at %s", resolved, other)
	}
}
