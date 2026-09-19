package staging

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

// fixture builds a modulesDir and hostDir tree, the latter containing a
// symlink into the former (like the generated mirror link), plus a
// hardware-configuration.nix and flake.lock. Returns their paths.
func fixture(t *testing.T) (modulesDir, hostDir, hardwareConfig, lockFile string) {
	t.Helper()
	root := t.TempDir()

	modulesDir = filepath.Join(root, "modules")
	if err := os.MkdirAll(filepath.Join(modulesDir, "system"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modulesDir, "system", "desktop.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}

	hostDir = filepath.Join(root, "host1")
	if err := os.MkdirAll(filepath.Join(hostDir, "modules"), 0755); err != nil {
		t.Fatal(err)
	}
	// Real file the mirror symlink would otherwise point at.
	if err := os.WriteFile(filepath.Join(hostDir, "modules", "default.nix"), []byte("{ imports = [ ./system/desktop.nix ]; }"), 0644); err != nil {
		t.Fatal(err)
	}
	// Mirror symlink under modulesDir, like links.EnsureMirror creates.
	if err := os.Symlink(filepath.Join(hostDir, "modules", "default.nix"), filepath.Join(modulesDir, "default.nix")); err != nil {
		t.Fatal(err)
	}

	// Local modules link under modulesDir, like links.EnsureLocalModules
	// creates: modulesDir/local -> hostDir/modules.
	if err := os.WriteFile(filepath.Join(hostDir, "modules", "foo.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(hostDir, "modules"), filepath.Join(modulesDir, "local")); err != nil {
		t.Fatal(err)
	}

	hardwareConfig = filepath.Join(root, "hardware-configuration.nix")
	if err := os.WriteFile(hardwareConfig, []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}

	lockFile = filepath.Join(root, "flake.lock")
	if err := os.WriteFile(lockFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	return modulesDir, hostDir, hardwareConfig, lockFile
}

func TestMaterialize_Basic(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, hardwareConfig, lockFile := fixture(t)

	if err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, lockFile); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	mustExist := []string{
		filepath.Join(stagingDir, Marker),
		filepath.Join(stagingDir, "framework", "system.nix"),
		filepath.Join(stagingDir, "framework", "shadow.sh"),
		filepath.Join(stagingDir, "framework", "units.nix"),
		filepath.Join(stagingDir, "framework", "overlay.nix"),
		filepath.Join(stagingDir, "flake.nix"),
		filepath.Join(stagingDir, "framework", "outputs.nix"),
		filepath.Join(stagingDir, "config", "modules", "system", "desktop.nix"),
		filepath.Join(stagingDir, "config", "modules", "default.nix"),
		filepath.Join(stagingDir, "config", "modules", "local", "foo.nix"),
		filepath.Join(stagingDir, "config", "local", "modules", "default.nix"),
		filepath.Join(stagingDir, "hardware-configuration.nix"),
		filepath.Join(stagingDir, "flake.lock"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	// No symlinks anywhere under stagingDir.
	err := filepath.WalkDir(stagingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			t.Errorf("symlink found under staging: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	// The dereferenced mirror link should now be a real file with the
	// host default.nix's content, not the host tree's own copy again.
	data, err := os.ReadFile(filepath.Join(stagingDir, "config", "modules", "default.nix"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "{ imports = [ ./system/desktop.nix ]; }" {
		t.Fatalf("mirror content = %q", data)
	}
}

func TestMaterialize_LockOptional(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, hardwareConfig, _ := fixture(t)
	missingLock := filepath.Join(t.TempDir(), "flake.lock")

	if err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, missingLock); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stagingDir, "flake.lock")); !os.IsNotExist(err) {
		t.Fatalf("flake.lock should not exist, err=%v", err)
	}
}

func TestMaterialize_GuardRefusesForeignDir(t *testing.T) {
	stagingDir := t.TempDir()
	// stagingDir exists (created by t.TempDir) but has no Marker file, and
	// contains an unrelated file, simulating a non-luxos directory.
	if err := os.WriteFile(filepath.Join(stagingDir, "not-ours.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	modulesDir, hostDir, hardwareConfig, lockFile := fixture(t)

	if err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, lockFile); err == nil {
		t.Fatalf("expected guard error for foreign staging dir")
	}

	if _, err := os.Stat(filepath.Join(stagingDir, "not-ours.txt")); err != nil {
		t.Fatalf("foreign dir should be untouched: %v", err)
	}
}

func TestMaterialize_GuardAllowsOwnTree(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, hardwareConfig, lockFile := fixture(t)

	if err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, lockFile); err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	if err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, lockFile); err != nil {
		t.Fatalf("second Materialize (re-run on own tree): %v", err)
	}
}

func TestMaterialize_DanglingLinkRefused(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, hardwareConfig, lockFile := fixture(t)

	dangling := filepath.Join(modulesDir, "broken.nix")
	if err := os.Symlink(filepath.Join(modulesDir, "does-not-exist.nix"), dangling); err != nil {
		t.Fatal(err)
	}

	err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, lockFile)
	if err == nil {
		t.Fatalf("expected dangling symlink error")
	}
	if _, statErr := os.Stat(stagingDir); !os.IsNotExist(statErr) {
		t.Fatalf("staging dir should not have been created on dangling-link error")
	}
}

func TestInstall(t *testing.T) {
	stagingDir := t.TempDir()

	if err := Install(stagingDir, "flake.nix", []byte("{ }")); err != nil {
		t.Fatalf("Install: %v", err)
	}

	path := filepath.Join(stagingDir, "flake.nix")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Fatalf("mode = %o, want 0644", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "{ }" {
		t.Fatalf("content = %q", data)
	}
}

func TestInstall_NestedRel(t *testing.T) {
	stagingDir := t.TempDir()

	if err := Install(stagingDir, filepath.Join("sub", "dir", "file.nix"), []byte("x")); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stagingDir, "sub", "dir", "file.nix")); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

// fakeLib is a minimal, network-free stand-in for nixpkgs' lib, providing
// only what framework/units.nix uses, so the eval below doesn't depend on
// <nixpkgs> being on NIX_PATH.
const fakeLib = `{
    hasSuffix = suffix: s:
      let l = builtins.stringLength suffix; sl = builtins.stringLength s; in
      sl >= l && builtins.substring (sl - l) l s == suffix;
    removeSuffix = suffix: s:
      let l = builtins.stringLength suffix; sl = builtins.stringLength s; in
      if sl >= l && builtins.substring (sl - l) l s == suffix
      then builtins.substring 0 (sl - l) s
      else s;
    foldl' = builtins.foldl';
    foldlAttrs = f: init: attrs:
      builtins.foldl' (acc: name: f acc name attrs.${name}) init (builtins.attrNames attrs);
  }`

// materializeUnitsFixture stages modulesDir (built by the caller) through
// Materialize and returns the staged framework/units.nix path and the
// staged config/modules path to evaluate it against.
func materializeUnitsFixture(t *testing.T, modulesDir string) (unitsNixPath, stagedModulesDir string) {
	t.Helper()
	root := t.TempDir()
	stagingDir := filepath.Join(root, "staging")

	hostDir := filepath.Join(root, "host1")
	if err := os.MkdirAll(hostDir, 0755); err != nil {
		t.Fatal(err)
	}
	hardwareConfig := filepath.Join(root, "hardware-configuration.nix")
	if err := os.WriteFile(hardwareConfig, []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, filepath.Join(root, "flake.lock")); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	return filepath.Join(stagingDir, "framework", "units.nix"), filepath.Join(stagingDir, "config", "modules")
}

// evalUnitsNix evaluates framework/units.nix against modulesDir with the
// given name list, returning the resolved paths as toString'd strings and
// whether evaluation succeeded (builtins.tryEval).
func evalUnitsNix(t *testing.T, unitsNixPath, modulesDir string, names []string) (paths []string, ok bool) {
	t.Helper()

	namesExpr := "[ "
	for _, n := range names {
		namesExpr += `"` + n + `" `
	}
	namesExpr += "]"

	expr := `
let
  lib = ` + fakeLib + `;
  f = import ` + nixQuote(unitsNixPath) + ` { inherit lib; root = ` + nixQuote(modulesDir) + `; };
  resolved = map toString (f ` + namesExpr + `);
  attempt = builtins.tryEval (builtins.deepSeq resolved resolved);
in
  { ok = attempt.success; paths = if attempt.success then attempt.value else []; }
`

	cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "--expr", expr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nix-instantiate --eval: %v\n%s", err, out)
	}

	var result struct {
		Ok    bool     `json:"ok"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("unmarshal eval output %q: %v", out, err)
	}
	return result.Paths, result.Ok
}

// nixQuote renders path as a double-quoted Nix string literal. Absolute
// paths on disk never contain a double quote or backslash, so no escaping
// is needed.
func nixQuote(path string) string {
	return `"` + path + `"`
}

func TestUnitsNix_ResolvesAndThrowsOnUnknown(t *testing.T) {
	skipIfNoNix(t)

	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	if err := os.WriteFile(mustJoin(t, modulesDir, "a.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustJoin(t, modulesDir, "cat", "b.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustJoin(t, modulesDir, "cat", "c", "default.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}

	unitsNixPath, stagedModulesDir := materializeUnitsFixture(t, modulesDir)

	paths, ok := evalUnitsNix(t, unitsNixPath, stagedModulesDir, []string{"b"})
	if !ok {
		t.Fatalf("evaluating [\"b\"] should have succeeded")
	}
	want := filepath.Join(stagedModulesDir, "cat", "b.nix")
	if len(paths) != 1 || paths[0] != want {
		t.Fatalf("paths = %v, want [%s]", paths, want)
	}

	if _, ok := evalUnitsNix(t, unitsNixPath, stagedModulesDir, []string{"does-not-exist"}); ok {
		t.Fatalf("evaluating an unknown name should have thrown")
	}
}

func TestUnitsNix_DuplicateNameThrows(t *testing.T) {
	skipIfNoNix(t)

	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	if err := os.WriteFile(mustJoin(t, modulesDir, "a.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mustJoin(t, modulesDir, "cat", "a.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}

	unitsNixPath, stagedModulesDir := materializeUnitsFixture(t, modulesDir)

	if _, ok := evalUnitsNix(t, unitsNixPath, stagedModulesDir, []string{"a"}); ok {
		t.Fatalf("evaluating a duplicate basename should have thrown")
	}
}

// mustJoin creates the parent directories for filepath.Join(base, elem...)
// and returns that path.
func mustJoin(t *testing.T, base string, elem ...string) string {
	t.Helper()
	full := filepath.Join(append([]string{base}, elem...)...)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestLockChanged(t *testing.T) {
	stagingDir := t.TempDir()
	hostLock := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(filepath.Join(stagingDir, "flake.lock"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	if changed, err := LockChanged(stagingDir, hostLock); err != nil || !changed {
		t.Errorf("missing host lock: changed=%v err=%v, want true", changed, err)
	}

	if err := os.WriteFile(hostLock, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if changed, err := LockChanged(stagingDir, hostLock); err != nil || changed {
		t.Errorf("same content: changed=%v err=%v, want false", changed, err)
	}

	if err := os.WriteFile(hostLock, []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
	if changed, err := LockChanged(stagingDir, hostLock); err != nil || !changed {
		t.Errorf("different content: changed=%v err=%v, want true", changed, err)
	}
}

func TestLockChanged_MissingStagedLockErrors(t *testing.T) {
	if _, err := LockChanged(t.TempDir(), filepath.Join(t.TempDir(), "flake.lock")); err == nil {
		t.Fatal("want error when the staged lock is missing")
	}
}

func TestCopyLockBack(t *testing.T) {
	stagingDir := t.TempDir()
	hostLock := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(filepath.Join(stagingDir, "flake.lock"), []byte("locked"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyLockBack(stagingDir, hostLock); err != nil {
		t.Fatalf("CopyLockBack: %v", err)
	}
	data, err := os.ReadFile(hostLock)
	if err != nil || string(data) != "locked" {
		t.Errorf("host lock = %q, err=%v, want %q", data, err, "locked")
	}
	info, err := os.Stat(hostLock)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fileMode {
		t.Errorf("host lock mode = %v, want %v", info.Mode().Perm(), fileMode)
	}
}
