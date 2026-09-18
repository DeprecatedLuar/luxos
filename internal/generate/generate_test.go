package generate

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

var update = flag.Bool("update", false, "rewrite golden files")

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func skipIfNoBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
}

// assertGolden compares got against testdata/<name>.golden, rewriting the
// golden file instead when -update is passed.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// assertNixParses writes content to a temp .nix file and checks it parses
// with nix-instantiate --parse.
func assertNixParses(t *testing.T, content []byte) {
	t.Helper()
	skipIfNoNix(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "check.nix")
	if err := os.WriteFile(file, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := nix.Parse(file); err != nil {
		t.Errorf("nix-instantiate --parse failed: %v", err)
	}
}

func testChannels() config.Channels {
	return config.Channels{
		Base: "stable",
		Channels: []config.Input{
			{Name: "stable", URL: "github:NixOS/nixpkgs/nixos-24.05"},
			{Name: "unstable", URL: "github:NixOS/nixpkgs/nixos-unstable"},
		},
		Flakes: []config.Input{
			{Name: "home-manager", URL: "github:nix-community/home-manager"},
		},
	}
}

func TestConfiguration_Golden(t *testing.T) {
	out, err := Configuration("paraloid", "/etc/luxos/bin/luxos")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	assertGolden(t, "configuration", out)
	assertNixParses(t, out)
}

func TestConfiguration_Deterministic(t *testing.T) {
	a, err := Configuration("paraloid", "/x/luxos")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	b, err := Configuration("paraloid", "/x/luxos")
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("Configuration is not deterministic")
	}
}

func TestFlake_Golden(t *testing.T) {
	out, err := Flake("paraloid", testChannels())
	if err != nil {
		t.Fatalf("Flake: %v", err)
	}
	assertGolden(t, "flake", out)
	assertNixParses(t, out)
}

func TestFlake_Deterministic(t *testing.T) {
	a, err := Flake("paraloid", testChannels())
	if err != nil {
		t.Fatalf("Flake: %v", err)
	}
	b, err := Flake("paraloid", testChannels())
	if err != nil {
		t.Fatalf("Flake: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("Flake is not deterministic")
	}
}

func TestFlake_EmptyHostErrors(t *testing.T) {
	if _, err := Flake("", testChannels()); err == nil {
		t.Fatalf("Flake: want error for empty host")
	}
}

func TestSystemNix_Parses(t *testing.T) {
	content, err := framework.File("system.nix")
	if err != nil {
		t.Fatalf("framework.File(system.nix): %v", err)
	}
	assertNixParses(t, content)
}

func TestSystemNix_NoWriteShellScriptBin(t *testing.T) {
	content, err := framework.File("system.nix")
	if err != nil {
		t.Fatalf("framework.File(system.nix): %v", err)
	}
	if strings.Contains(string(content), "writeShellScriptBin") {
		t.Errorf("system.nix still references writeShellScriptBin; that belongs in configuration.nix now")
	}
}

func TestShadowScript_BashSyntaxValid(t *testing.T) {
	skipIfNoBash(t)

	body, err := framework.File("shadow.sh")
	if err != nil {
		t.Fatalf("framework.File(shadow.sh): %v", err)
	}

	script := "LUXOS_ARGS=(rebuild)\nREAL=/bin/true\n" + string(body)

	dir := t.TempDir()
	file := filepath.Join(dir, "shim.sh")
	if err := os.WriteFile(file, []byte(script), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cmd := exec.Command("bash", "-n", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("bash -n failed: %v\n%s", err, out)
	}
}

func TestLockDrift_NoLockFile(t *testing.T) {
	orphaned, unlocked, err := LockDrift(filepath.Join(t.TempDir(), "flake.lock"), []string{"nixpkgs"})
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if orphaned != nil || unlocked != nil {
		t.Errorf("LockDrift with missing lock: got orphaned=%v unlocked=%v, want nil, nil", orphaned, unlocked)
	}
}

func TestLockDrift_InSync(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "flake.lock")
	mustWrite(t, lock, `{"nodes":{"root":{"inputs":{"nixpkgs":"nixpkgs_2","unstable":"unstable_2"}}}}`)

	orphaned, unlocked, err := LockDrift(lock, []string{"nixpkgs", "unstable"})
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if len(orphaned) != 0 || len(unlocked) != 0 {
		t.Errorf("LockDrift in sync: got orphaned=%v unlocked=%v, want none", orphaned, unlocked)
	}
}

func TestLockDrift_Orphaned(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "flake.lock")
	mustWrite(t, lock, `{"nodes":{"root":{"inputs":{"nixpkgs":"nixpkgs_2","old-channel":"old_2"}}}}`)

	orphaned, unlocked, err := LockDrift(lock, []string{"nixpkgs"})
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if want := []string{"old-channel"}; !equalStrings(orphaned, want) {
		t.Errorf("orphaned = %v, want %v", orphaned, want)
	}
	if len(unlocked) != 0 {
		t.Errorf("unlocked = %v, want none", unlocked)
	}
}

func TestLockDrift_Unlocked(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "flake.lock")
	mustWrite(t, lock, `{"nodes":{"root":{"inputs":{"nixpkgs":"nixpkgs_2"}}}}`)

	orphaned, unlocked, err := LockDrift(lock, []string{"nixpkgs", "new-channel"})
	if err != nil {
		t.Fatalf("LockDrift: %v", err)
	}
	if len(orphaned) != 0 {
		t.Errorf("orphaned = %v, want none", orphaned)
	}
	if want := []string{"new-channel"}; !equalStrings(unlocked, want) {
		t.Errorf("unlocked = %v, want %v", unlocked, want)
	}
}

func TestLockInputs(t *testing.T) {
	got := LockInputs(testChannels())
	want := []string{"nixpkgs", "unstable", "home-manager"}
	if !equalStrings(got, want) {
		t.Errorf("LockInputs = %v, want %v", got, want)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

