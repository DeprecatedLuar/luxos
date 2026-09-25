package staging

import (
	"encoding/json"
	"errors"
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
// flake.lock. Returns their paths.
func fixture(t *testing.T) (modulesDir, hostDir, lockFile, environmentFile string) {
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

	lockFile = filepath.Join(root, "flake.lock")
	if err := os.WriteFile(lockFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	environmentFile = filepath.Join(root, "environment")
	if err := os.WriteFile(environmentFile, []byte("A=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	return modulesDir, hostDir, lockFile, environmentFile
}

func TestMaterialize_Basic(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, lockFile, environmentFile := fixture(t)

	if err := Materialize(stagingDir, modulesDir, hostDir, lockFile, environmentFile); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	mustExist := []string{
		filepath.Join(stagingDir, "framework", "system.nix"),
		filepath.Join(stagingDir, "framework", "shadow.sh"),
		filepath.Join(stagingDir, "framework", "units.nix"),
		filepath.Join(stagingDir, "framework", "overlay.nix"),
		filepath.Join(stagingDir, "flake.nix"),
		filepath.Join(stagingDir, "framework", "outputs.nix"),
		filepath.Join(stagingDir, "framework", "environment.nix"),
		filepath.Join(stagingDir, "config", "environment"),
		filepath.Join(stagingDir, "config", "modules", "system", "desktop.nix"),
		filepath.Join(stagingDir, "config", "modules", "default.nix"),
		filepath.Join(stagingDir, "config", "modules", "local", "foo.nix"),
		filepath.Join(stagingDir, "config", "local", "modules", "default.nix"),
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
	modulesDir, hostDir, _, environmentFile := fixture(t)
	missingLock := filepath.Join(t.TempDir(), "flake.lock")

	if err := Materialize(stagingDir, modulesDir, hostDir, missingLock, environmentFile); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stagingDir, "flake.lock")); !os.IsNotExist(err) {
		t.Fatalf("flake.lock should not exist, err=%v", err)
	}
}

func TestMaterialize_MissingEnvironmentFails(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, lockFile, _ := fixture(t)
	missingEnv := filepath.Join(t.TempDir(), "environment")

	if err := Materialize(stagingDir, modulesDir, hostDir, lockFile, missingEnv); err == nil {
		t.Fatal("expected an error for a missing environment file")
	}
}

func TestMaterialize_LeavesComputerFiles(t *testing.T) {
	stagingDir := t.TempDir()
	modulesDir, hostDir, lockFile, environmentFile := fixture(t)
	for _, name := range []string{"hardware-configuration.nix", "boot.nix"} {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 2; i++ {
		if err := Materialize(stagingDir, modulesDir, hostDir, lockFile, environmentFile); err != nil {
			t.Fatalf("Materialize #%d: %v", i, err)
		}
	}

	for _, name := range []string{"hardware-configuration.nix", "boot.nix"} {
		data, err := os.ReadFile(filepath.Join(stagingDir, name))
		if err != nil || string(data) != "keep" {
			t.Errorf("%s = %q, %v; want untouched", name, data, err)
		}
	}
}

func TestMaterialize_DanglingLinkRefused(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, lockFile, environmentFile := fixture(t)

	dangling := filepath.Join(modulesDir, "broken.nix")
	if err := os.Symlink(filepath.Join(modulesDir, "does-not-exist.nix"), dangling); err != nil {
		t.Fatal(err)
	}

	err := Materialize(stagingDir, modulesDir, hostDir, lockFile, environmentFile)
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
	environmentFile := filepath.Join(root, "environment")
	if err := os.WriteFile(environmentFile, []byte("A=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Materialize(stagingDir, modulesDir, hostDir, filepath.Join(root, "flake.lock"), environmentFile); err != nil {
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

const luxosLockFixture = `{"nodes":{"luxos":{"locked":{"lastModified":1790088510,"narHash":"sha256-7Qp0Ew+0CZeNZjnKSuWI4GTdNllVrPPYzAcTcSGAybw=","owner":"DeprecatedLuar","repo":"luxos","rev":"769dcdb00b27f4aea190db70d1a9d65d858c788d","type":"github"},"original":{"owner":"DeprecatedLuar","ref":"main","repo":"luxos","type":"github"}},"root":{"inputs":{"luxos":"luxos"}}},"root":"root","version":7}`

func writeLock(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "flake.lock")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadLockInput(t *testing.T) {
	got, ok, err := ReadLockInput(writeLock(t, luxosLockFixture), "luxos")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	want := LockInput{Type: "github", Owner: "DeprecatedLuar", Repo: "luxos", Rev: "769dcdb00b27f4aea190db70d1a9d65d858c788d"}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReadLockInputAbsentNode(t *testing.T) {
	_, ok, err := ReadLockInput(writeLock(t, luxosLockFixture), "nixpkgs")
	if ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

func TestReadLockInputMissingFile(t *testing.T) {
	_, ok, err := ReadLockInput(filepath.Join(t.TempDir(), "nope.lock"), "luxos")
	if ok || err != nil {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

func TestReadLockInputMalformed(t *testing.T) {
	if _, _, err := ReadLockInput(writeLock(t, "{not json"), "luxos"); err == nil {
		t.Error("want error")
	}
}

const testHeader = "# LUXOS property - keep walking buddy\n"

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func adoptedFiles() map[string]string {
	return map[string]string{
		"framework/system.nix":        testHeader + "{ }",
		"config/x.nix":                "{ }",
		"flake.nix":                   "{ }",
		"framework/flake-file.nix":    "{ }",
		"framework/configuration.nix": "{ }",
		"flake.lock":                  "{ }",
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestAdopt_EmptyDir(t *testing.T) {
	stage, backup := t.TempDir(), filepath.Join(t.TempDir(), "backup")
	changes, err := Adopt(stage, backup)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
	if exists(backup) {
		t.Fatal("backup dir created for nothing")
	}
}

func TestAdopt_AlreadyAdoptedNoStrangers(t *testing.T) {
	stage, backup := t.TempDir(), filepath.Join(t.TempDir(), "backup")
	writeTree(t, stage, adoptedFiles())
	changes, err := Adopt(stage, backup)
	if err != nil || len(changes) != 0 {
		t.Fatalf("changes=%v err=%v", changes, err)
	}
	if exists(backup) {
		t.Fatal("backup dir created for nothing")
	}
}

func TestAdopt_MovesStranger(t *testing.T) {
	stage, backup := t.TempDir(), filepath.Join(t.TempDir(), "backup")
	files := adoptedFiles()
	files["strange.nix"] = "strange"
	writeTree(t, stage, files)
	changes, err := Adopt(stage, backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != "moved" || changes[0].Path != filepath.Join(stage, "strange.nix") {
		t.Fatalf("changes = %+v", changes)
	}
	if exists(filepath.Join(stage, "strange.nix")) {
		t.Fatal("stranger still in staging")
	}
	if b, err := os.ReadFile(changes[0].Dest); err != nil || string(b) != "strange" {
		t.Fatalf("moved file: %q %v", b, err)
	}
	if filepath.Dir(filepath.Dir(changes[0].Dest)) != backup {
		t.Fatalf("dest %q not in a timestamped leaf of %q", changes[0].Dest, backup)
	}
	for name := range adoptedFiles() {
		if !exists(filepath.Join(stage, name)) {
			t.Fatalf("%s was moved", name)
		}
	}
}

func TestAdopt_VanillaTree(t *testing.T) {
	stage, backup := t.TempDir(), filepath.Join(t.TempDir(), "backup")
	writeTree(t, stage, map[string]string{
		"configuration.nix":          "{ }",
		"hardware-configuration.nix": "{ }",
		"flake.nix":                  "{ }",
	})
	changes, err := Adopt(stage, backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	if exists(filepath.Join(stage, "configuration.nix")) || exists(filepath.Join(stage, "flake.nix")) {
		t.Fatal("user config left in staging")
	}
	if !exists(filepath.Join(stage, "hardware-configuration.nix")) {
		t.Fatal("hardware-configuration.nix was moved")
	}
}

func TestAdopt_SystemNixWithoutHeaderIsNotAdopted(t *testing.T) {
	stage, backup := t.TempDir(), filepath.Join(t.TempDir(), "backup")
	writeTree(t, stage, map[string]string{"framework/system.nix": "{ }\n", "flake.nix": "{ }"})
	changes, err := Adopt(stage, backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || exists(filepath.Join(stage, "framework")) {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestAdopt_NoBackupDir(t *testing.T) {
	stage := t.TempDir()
	writeTree(t, stage, map[string]string{"configuration.nix": "{ }"})
	if _, err := Adopt(stage, ""); !errors.Is(err, ErrNoBackupDir) {
		t.Fatalf("err = %v, want ErrNoBackupDir", err)
	}
	if !exists(filepath.Join(stage, "configuration.nix")) {
		t.Fatal("file moved despite error")
	}
}

func TestAdopt_SymlinkedStaging(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "nixos")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	changes, err := Adopt(link, filepath.Join(base, "backup"))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != "healed" {
		t.Fatalf("changes = %+v", changes)
	}
	if info, err := os.Lstat(link); err != nil || !info.IsDir() {
		t.Fatalf("staging is not a real directory: %v", err)
	}
}

func TestAdopt_ChownWalkDoesNotFollowSymlink(t *testing.T) {
	stage, backup := t.TempDir(), filepath.Join(t.TempDir(), "backup")
	outside := t.TempDir()
	target := filepath.Join(outside, "t.txt")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stage, "dir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(stage, "dir", "link")); err != nil {
		t.Fatal(err)
	}
	changes, err := Adopt(stage, backup)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(changes[0].Dest, "link")
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link not preserved as symlink: %v", err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "keep" {
		t.Fatalf("target changed: %q %v", b, err)
	}
}

func TestCopyTreePreservesSymlinks(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	writeTree(t, src, map[string]string{"a/f.txt": "x"})
	if err := os.Symlink("/nonexistent", filepath.Join(src, "l")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(base, "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(dst, "l")); err != nil || got != "/nonexistent" {
		t.Fatalf("readlink = %q %v", got, err)
	}
	if b, err := os.ReadFile(filepath.Join(dst, "a", "f.txt")); err != nil || string(b) != "x" {
		t.Fatalf("copied file: %q %v", b, err)
	}
}

func TestPrune(t *testing.T) {
	stage := t.TempDir()
	files := adoptedFiles()
	files["hardware-configuration.nix"] = "{ }"
	files["boot.nix"] = "{ }"
	writeTree(t, stage, files)
	if err := Prune(stage); err != nil {
		t.Fatal(err)
	}
	for _, name := range owned {
		if exists(filepath.Join(stage, name)) {
			t.Fatalf("%s survived", name)
		}
	}
	for _, name := range preserved {
		if !exists(filepath.Join(stage, name)) {
			t.Fatalf("%s removed", name)
		}
	}
	if err := Prune(stage); err != nil {
		t.Fatalf("second Prune: %v", err)
	}
}

func TestSeal(t *testing.T) {
	stage := t.TempDir()
	writeTree(t, stage, map[string]string{
		"framework/system.nix":   "x",
		"config/modules/a/b.nix": "x",
		"flake.nix":              "x",
		"boot.nix":               "x",
	})
	if err := Seal(stage); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"framework/system.nix", "config/modules/a/b.nix", "flake.nix"} {
		if fi, _ := os.Stat(filepath.Join(stage, f)); fi.Mode().Perm() != 0444 {
			t.Errorf("%s mode %v", f, fi.Mode().Perm())
		}
	}
	for _, d := range []string{"framework", "config", "config/modules", "config/modules/a"} {
		if fi, _ := os.Stat(filepath.Join(stage, d)); fi.Mode().Perm() != 0755 {
			t.Errorf("%s mode %v", d, fi.Mode().Perm())
		}
	}
	if fi, _ := os.Stat(filepath.Join(stage, "boot.nix")); fi.Mode().Perm() == 0444 {
		t.Error("boot.nix was sealed")
	}
	if err := Prune(stage); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(stage, "framework")) || exists(filepath.Join(stage, "config")) || exists(filepath.Join(stage, "flake.nix")) {
		t.Error("Prune left sealed entries")
	}
	if !exists(filepath.Join(stage, "boot.nix")) {
		t.Error("Prune removed boot.nix")
	}
}
