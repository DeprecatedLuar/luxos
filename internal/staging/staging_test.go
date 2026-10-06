package staging

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/hardware"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

const testNixpkgs = "github:NixOS/nixpkgs/nixos-25.11"

// hwDirOf is the hardware folder fixture builds next to hostDir.
func hwDirOf(hostDir string) string {
	return filepath.Join(filepath.Dir(hostDir), "hardware")
}

// materialize stages hostDir against modulesDir through Materialize with just
// a nixpkgs pin at nixpkgsURL ("" declares no input), no GPUs, and us as the
// host's modules.
func materialize(stagingDir, modulesDir, hostDir string, us []modules.Module, environmentFile, nixpkgsURL string) error {
	h := &modules.Host{Name: filepath.Base(hostDir), ModulesDir: modulesDir, HostDir: hostDir, Modules: us}
	var inputs []nix.InputDecl
	if nixpkgsURL != "" {
		inputs = []nix.InputDecl{{Name: "nixpkgs", URL: nixpkgsURL, Value: map[string]any{"url": nixpkgsURL}}}
	}
	return Materialize(stagingDir, h, hwDirOf(hostDir), environmentFile, inputs, hardware.Facts{}, nil)
}

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

// fixture builds a modulesDir and hostDir tree with the generated mirror and
// local links under modulesDir, a hardware folder, and the host's flake.lock.
// Returns their paths.
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
	// The host's selection, which the mirror symlink points at.
	if err := os.WriteFile(filepath.Join(hostDir, "modules.nix"), []byte("{ imports = [ ./system/desktop.nix ]; }"), 0644); err != nil {
		t.Fatal(err)
	}
	// Mirror symlink under modulesDir, like config.EnsureMirror creates.
	if err := os.Symlink(filepath.Join(hostDir, "modules.nix"), filepath.Join(modulesDir, "default.nix")); err != nil {
		t.Fatal(err)
	}

	// Local modules link under modulesDir, like config.EnsureLocalModules
	// creates: modulesDir/local -> hostDir/modules.
	if err := os.WriteFile(filepath.Join(hostDir, "modules", "foo.nix"), []byte("{ }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(hostDir, "modules"), filepath.Join(modulesDir, "local")); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(hostDir, "machine.nix"), []byte("{ machine = 1; }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostDir, ".plsdonttouch.nix"), []byte("{ state = 1; }"), 0644); err != nil {
		t.Fatal(err)
	}

	lockFile = filepath.Join(hostDir, "flake.lock")
	if err := os.WriteFile(lockFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(hwDirOf(hostDir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hwDirOf(hostDir), "default.nix"), []byte("{ hw = 1; }"), 0644); err != nil {
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
	modulesDir, hostDir, _, environmentFile := fixture(t)

	if err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, testNixpkgs); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	mustExist := []string{
		filepath.Join(stagingDir, "framework", "system.nix"),
		filepath.Join(stagingDir, "framework", "units.nix"),
		filepath.Join(stagingDir, "framework", "overlay.nix"),
		filepath.Join(stagingDir, "flake.nix"),
		filepath.Join(stagingDir, "framework", "outputs.nix"),
		filepath.Join(stagingDir, "framework", "environment.nix"),
		filepath.Join(stagingDir, "framework", "luxos-hardware-defaults.nix"),
		filepath.Join(stagingDir, "framework", "nvidia-generations.nix"),
		filepath.Join(stagingDir, "framework", "flake-file.nix"),
		filepath.Join(stagingDir, "framework", "hardware-facts.nix"),
		filepath.Join(stagingDir, "framework", "configuration.nix"),
		filepath.Join(stagingDir, "config", "modules", "local", "hardware-support", "default.nix"),
		filepath.Join(stagingDir, "config", "environment"),
		filepath.Join(stagingDir, "config", "modules", "system", "desktop.nix"),
		filepath.Join(stagingDir, "config", "modules", "default.nix"),
		filepath.Join(stagingDir, "config", "modules", "local", "foo.nix"),
		filepath.Join(stagingDir, "config", "machine.nix"),
		filepath.Join(stagingDir, "config", ".plsdonttouch.nix"),
		filepath.Join(stagingDir, "flake.lock"),
	}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	if _, err := os.Stat(filepath.Join(stagingDir, "config", "local")); !os.IsNotExist(err) {
		t.Errorf("config/local must not exist: %v", err)
	}
	for name, want := range map[string]string{"machine.nix": "{ machine = 1; }", ".plsdonttouch.nix": "{ state = 1; }"} {
		got, err := os.ReadFile(filepath.Join(stagingDir, "config", name))
		if err != nil || string(got) != want {
			t.Errorf("config/%s = %q, %v", name, got, err)
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
	modulesDir, hostDir, lockFile, environmentFile := fixture(t)
	if err := os.Remove(lockFile); err != nil {
		t.Fatal(err)
	}

	if err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, testNixpkgs); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stagingDir, "flake.lock")); !os.IsNotExist(err) {
		t.Fatalf("flake.lock should not exist, err=%v", err)
	}
}

func TestMaterialize_MissingEnvironmentFails(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, _, _ := fixture(t)
	missingEnv := filepath.Join(t.TempDir(), "environment")

	if err := materialize(stagingDir, modulesDir, hostDir, nil, missingEnv, testNixpkgs); err == nil {
		t.Fatal("expected an error for a missing environment file")
	}
}

func TestMaterialize_LeavesComputerFiles(t *testing.T) {
	stagingDir := t.TempDir()
	modulesDir, hostDir, _, environmentFile := fixture(t)
	for _, name := range []string{"hardware-configuration.nix", "boot.nix"} {
		if err := os.WriteFile(filepath.Join(stagingDir, name), []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 2; i++ {
		if err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, testNixpkgs); err != nil {
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
	modulesDir, hostDir, _, environmentFile := fixture(t)

	dangling := filepath.Join(modulesDir, "broken.nix")
	if err := os.Symlink(filepath.Join(modulesDir, "does-not-exist.nix"), dangling); err != nil {
		t.Fatal(err)
	}

	err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, testNixpkgs)
	if err == nil {
		t.Fatalf("expected dangling symlink error")
	}
	if _, statErr := os.Stat(stagingDir); !os.IsNotExist(statErr) {
		t.Fatalf("staging dir should not have been created on dangling-link error")
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
	for _, name := range []string{"machine.nix", ".plsdonttouch.nix", "modules.nix"} {
		if err := os.WriteFile(filepath.Join(hostDir, name), []byte("{ }"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(hwDirOf(hostDir), 0755); err != nil {
		t.Fatal(err)
	}
	environmentFile := filepath.Join(root, "environment")
	if err := os.WriteFile(environmentFile, []byte("A=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, testNixpkgs); err != nil {
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
	if len(changes) != 3 {
		t.Fatalf("changes = %+v", changes)
	}
	for _, name := range []string{"configuration.nix", "flake.nix", "hardware-configuration.nix"} {
		if exists(filepath.Join(stage, name)) {
			t.Fatalf("%s left in staging", name)
		}
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
	files["stranger.nix"] = "{ }"
	writeTree(t, stage, files)
	if err := Prune(stage); err != nil {
		t.Fatal(err)
	}
	for _, name := range owned {
		if exists(filepath.Join(stage, name)) {
			t.Fatalf("%s survived", name)
		}
	}
	if !exists(filepath.Join(stage, "stranger.nix")) {
		t.Fatal("stranger.nix removed")
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

func TestMaterialize_ShadowStagedAtOriginalLocation(t *testing.T) {
	skipIfNoNix(t)
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, _, environmentFile := fixture(t)

	write := func(p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(modulesDir, "desktop", "shells", "ambxst", "default.nix"), "{ shared = 1; }")
	write(filepath.Join(hostDir, "modules", "ambxst.nix"), "{ local = 1; }")
	write(filepath.Join(hostDir, "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./desktop/shells/ambxst\n    ./local/ambxst.nix\n    ./system/desktop.nix\n  ];\n}\n")

	us := []modules.Module{
		{Name: "ambxst", Path: "local/ambxst.nix", Shadows: "desktop/shells/ambxst", Abs: filepath.Join(hostDir, "modules", "ambxst.nix")},
		{Name: "desktop", Path: "system/desktop.nix", Abs: filepath.Join(modulesDir, "system", "desktop.nix")},
	}
	if err := materialize(stagingDir, modulesDir, hostDir, us, environmentFile, testNixpkgs); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	mods := filepath.Join(stagingDir, "config", "modules")
	got, err := os.ReadFile(filepath.Join(mods, "desktop", "shells", "ambxst.nix"))
	if err != nil || string(got) != "{ local = 1; }" {
		t.Errorf("staged shadow = %q, %v", got, err)
	}
	for _, gone := range []string{"desktop/shells/ambxst", "local/ambxst.nix"} {
		if _, err := os.Stat(filepath.Join(mods, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should not be staged (err=%v)", gone, err)
		}
	}
	entry, err := os.ReadFile(filepath.Join(mods, "default.nix"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{ ... }:\n{\n  imports = [\n    ./desktop/shells/ambxst.nix\n    ./desktop/shells/ambxst.nix\n    ./system/desktop.nix\n  ];\n}\n"
	if string(entry) != want {
		t.Errorf("staged default.nix = %q, want %q", entry, want)
	}
	src, _ := os.ReadFile(filepath.Join(hostDir, "modules.nix"))
	if !strings.Contains(string(src), "./local/ambxst.nix") {
		t.Errorf("source modules.nix was rewritten: %q", src)
	}
}

func TestMaterialize_ShadowBundleStagedAtOriginalLocation(t *testing.T) {
	skipIfNoNix(t)
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, _, environmentFile := fixture(t)

	write := func(p, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(modulesDir, "users", "eduardo", "default.nix"), "{ shared = 1; }")
	write(filepath.Join(modulesDir, "users", "eduardo", "modules", "zsh.nix"), "{ shared = 2; }")
	write(filepath.Join(hostDir, "modules", "eduardo", "default.nix"), "{ local = 1; }")
	write(filepath.Join(hostDir, "modules", "eduardo", "modules", "git.nix"), "{ local = 2; }")
	write(filepath.Join(hostDir, "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/eduardo\n    ./local/eduardo/modules/git.nix\n    ./system/desktop.nix\n  ];\n}\n")

	us := []modules.Module{
		{Name: "eduardo", Path: "local/eduardo", Shadows: "users/eduardo", Abs: filepath.Join(hostDir, "modules", "eduardo")},
		{Name: "eduardo/git", Path: "local/eduardo/modules/git.nix", Shadows: "users/eduardo/modules/git.nix", Abs: filepath.Join(hostDir, "modules", "eduardo", "modules", "git.nix")},
		{Name: "desktop", Path: "system/desktop.nix", Abs: filepath.Join(modulesDir, "system", "desktop.nix")},
	}
	if err := materialize(stagingDir, modulesDir, hostDir, us, environmentFile, testNixpkgs); err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	mods := filepath.Join(stagingDir, "config", "modules")
	for rel, want := range map[string]string{
		"users/eduardo/default.nix":     "{ local = 1; }",
		"users/eduardo/modules/git.nix": "{ local = 2; }",
	} {
		got, err := os.ReadFile(filepath.Join(mods, rel))
		if err != nil || string(got) != want {
			t.Errorf("staged %s = %q, %v", rel, got, err)
		}
	}
	for _, gone := range []string{"users/eduardo/modules/zsh.nix", "local/eduardo"} {
		if _, err := os.Stat(filepath.Join(mods, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should not be staged (err=%v)", gone, err)
		}
	}
	entry, err := os.ReadFile(filepath.Join(mods, "default.nix"))
	if err != nil {
		t.Fatal(err)
	}
	want := "{ ... }:\n{\n  imports = [\n    ./users/eduardo\n    ./users/eduardo/modules/git.nix\n    ./system/desktop.nix\n  ];\n}\n"
	if string(entry) != want {
		t.Errorf("staged default.nix = %q, want %q", entry, want)
	}
}

func TestMaterializeRendersFlakeNix(t *testing.T) {
	modulesDir, hostDir, _, environmentFile := fixture(t)
	stagingDir := t.TempDir()
	const url = "github:NixOS/nixpkgs/nixos-99.99"
	if err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, url); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(stagingDir, "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`nixpkgs.url = "` + url + `";`, "github:denful/flake-file/"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("flake.nix lacks %q:\n%s", want, data)
		}
	}
}

func TestMaterializeEmptyNixpkgsErrors(t *testing.T) {
	modulesDir, hostDir, _, environmentFile := fixture(t)
	if err := materialize(t.TempDir(), modulesDir, hostDir, nil, environmentFile, ""); err == nil {
		t.Fatal("expected error for empty base channel url")
	}
}

func prevFixture(t *testing.T) (stage, prev string) {
	t.Helper()
	root := t.TempDir()
	stage = filepath.Join(root, "etc")
	prev = filepath.Join(root, "var", "prev")
	for p, c := range map[string]string{
		"flake.nix":              "flake",
		"flake.lock":             "lock",
		"framework/system.nix":   "sys",
		"config/modules/a/b.nix": "b",
		"stranger.txt":           "keep",
	} {
		full := filepath.Join(stage, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return stage, prev
}

func readStage(t *testing.T, stage, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(stage, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPrevious_RoundTrip(t *testing.T) {
	stage, prev := prevFixture(t)
	if err := SavePrevious(stage, prev); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(prev, "stranger.txt")); err == nil {
		t.Error("stranger copied into prev")
	}
	if err := os.WriteFile(filepath.Join(stage, "config/modules/a/b.nix"), []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "config/extra.nix"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RestorePrevious(stage, prev); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{
		"flake.nix": "flake", "flake.lock": "lock", "framework/system.nix": "sys",
		"config/modules/a/b.nix": "b", "stranger.txt": "keep",
	} {
		if got := readStage(t, stage, rel); got != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(stage, "config/extra.nix")); err == nil {
		t.Error("extra file survived restore")
	}
	if _, err := os.Stat(prev); err == nil {
		t.Error("prev not removed")
	}
}

func TestSavePrevious_NoopWhenExists(t *testing.T) {
	stage, prev := prevFixture(t)
	if err := os.MkdirAll(prev, 0755); err != nil {
		t.Fatal(err)
	}
	if err := SavePrevious(stage, prev); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(prev)
	if len(entries) != 0 {
		t.Errorf("prev modified: %v", entries)
	}
}

func TestRestorePrevious_NoopWhenMissing(t *testing.T) {
	stage, prev := prevFixture(t)
	if err := RestorePrevious(stage, prev); err != nil {
		t.Fatal(err)
	}
	if got := readStage(t, stage, "flake.nix"); got != "flake" {
		t.Errorf("stage changed: %q", got)
	}
}

func TestDropPrevious(t *testing.T) {
	stage, prev := prevFixture(t)
	if err := SavePrevious(stage, prev); err != nil {
		t.Fatal(err)
	}
	if err := DropPrevious(prev); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(prev); err == nil {
		t.Error("prev still exists")
	}
}

func TestMaterializeWritesEveryFrameworkFile(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, _, environmentFile := fixture(t)
	if err := materialize(stagingDir, modulesDir, hostDir, nil, environmentFile, testNixpkgs); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"system", "units", "overlay", "outputs", "environment", "luxos-hardware", "luxos-hardware-defaults",
		"flake-file", "hardware-facts", "configuration",
	} {
		if _, err := os.Stat(filepath.Join(stagingDir, "framework", name+".nix")); err != nil {
			t.Errorf("framework/%s.nix: %v", name, err)
		}
	}
	for rel, wantFrom := range map[string]string{
		"config/modules/default.nix":                        filepath.Join(hostDir, "modules.nix"),
		"config/modules/local/hardware-support/default.nix": filepath.Join(hwDirOf(hostDir), "default.nix"),
	} {
		got, err := os.ReadFile(filepath.Join(stagingDir, rel))
		want, werr := os.ReadFile(wantFrom)
		if err != nil || werr != nil || string(got) != string(want) {
			t.Errorf("%s = %q, %v; want the content of %s", rel, got, err, wantFrom)
		}
	}
	config, err := os.ReadFile(filepath.Join(stagingDir, "framework", "configuration.nix"))
	if err != nil || !strings.Contains(string(config), "host1") {
		t.Errorf("configuration.nix does not name the host: %q, %v", config, err)
	}
}

func TestMaterializeIgnoresRootLinks(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, _, environmentFile := fixture(t)
	other := filepath.Join(filepath.Dir(hostDir), "host2")
	for rel, content := range map[string]string{
		"modules.nix":        "{ imports = [ ]; }",
		"machine.nix":        "{ }",
		".plsdonttouch.nix":  "{ }",
		"modules/theirs.nix": "{ }",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(other, rel)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// modulesDir/local and modulesDir/default.nix point at host1; staging host2 must not see host1.
	if err := materialize(stagingDir, modulesDir, other, nil, environmentFile, testNixpkgs); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(stagingDir, "config", "modules")
	if _, err := os.Stat(filepath.Join(mods, "local", "theirs.nix")); err != nil {
		t.Errorf("host2's own module not staged: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mods, "local", "foo.nix")); !os.IsNotExist(err) {
		t.Errorf("host1's module leaked into host2's stage (err=%v)", err)
	}
	got, _ := os.ReadFile(filepath.Join(mods, "default.nix"))
	if string(got) != "{ imports = [ ]; }" {
		t.Errorf("default.nix = %q, want host2's selection", got)
	}
}

func TestMaterialize_StagesSettings(t *testing.T) {
	stagingDir := filepath.Join(t.TempDir(), "staging")
	modulesDir, hostDir, _, environmentFile := fixture(t)
	settingsFile := filepath.Join(hostDir, "settings", "eduardo", "git.nix")
	if err := os.MkdirAll(filepath.Dir(settingsFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsFile, []byte("{ eduardo.git.name = \"e\"; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h := &modules.Host{Name: filepath.Base(hostDir), ModulesDir: modulesDir, HostDir: hostDir}
	inputs := []nix.InputDecl{{Name: "nixpkgs", URL: testNixpkgs, Value: map[string]any{"url": testNixpkgs}}}
	if err := Materialize(stagingDir, h, hwDirOf(hostDir), environmentFile, inputs, hardware.Facts{}, []string{"eduardo/git"}); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	staged, err := os.ReadFile(filepath.Join(stagingDir, "config", "settings", "eduardo", "git.nix"))
	if err != nil || !strings.Contains(string(staged), "eduardo.git.name") {
		t.Errorf("staged settings = %q, %v", staged, err)
	}
	conf, err := os.ReadFile(filepath.Join(stagingDir, "framework", "configuration.nix"))
	if err != nil || !strings.Contains(string(conf), "../config/settings/eduardo/git.nix") {
		t.Errorf("configuration.nix does not import the settings file:\n%s", conf)
	}
}
