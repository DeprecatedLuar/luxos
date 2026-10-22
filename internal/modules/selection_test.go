package modules

import (
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---- Retarget ----

const sharedScope = config.ScopeShared

func sharedFiles(files ...string) []SelectionFile {
	out := make([]SelectionFile, len(files))
	for i, f := range files {
		out[i] = SelectionFile{Scope: sharedScope, File: f}
	}
	return out
}

func setupTwoHosts(t *testing.T) (host1, host2 string) {
	t.Helper()
	hostsDir := t.TempDir()
	host1 = filepath.Join(hostsDir, "host1", "modules", "default.nix")
	host2 = filepath.Join(hostsDir, "host2", "modules", "default.nix")
	mustWriteFile(t, host1, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	mustWriteFile(t, host2, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	return
}

func TestRetarget_Rename(t *testing.T) {
	skipIfNoNix(t)
	host1, host2 := setupTwoHosts(t)
	files := sharedFiles(host1, host2)

	changes, _, err := retarget(files, "foo", "b/foo.nix")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 entries", changes)
	}

	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f, sharedScope)
		if err != nil {
			t.Fatalf("ReadSelection(%s): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/foo.nix"}) {
			t.Errorf("ReadSelection(%s) = %v", f, got)
		}
	}

	// Second run: no changes (idempotent).
	changes2, _, err := retarget(files, "foo", "b/foo.nix")
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_Delete(t *testing.T) {
	skipIfNoNix(t)
	host1, host2 := setupTwoHosts(t)
	files := sharedFiles(host1, host2)

	changes, _, err := retarget(files, "foo", "")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 entries", changes)
	}
	for _, c := range changes {
		if c.New != "" {
			t.Errorf("Change.New = %q, want empty (removed)", c.New)
		}
	}

	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f, sharedScope)
		if err != nil {
			t.Fatalf("ReadSelection(%s): %v", f, err)
		}
		if len(got) != 0 {
			t.Errorf("ReadSelection(%s) = %v, want empty", f, got)
		}
	}

	changes2, _, err := retarget(files, "foo", "")
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_OnlyGivenFiles(t *testing.T) {
	skipIfNoNix(t)
	host1, host2 := setupTwoHosts(t)

	changes, _, err := retarget(sharedFiles(host1), "foo", "b/foo.nix")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host1 {
		t.Fatalf("changes = %v, want exactly one change to host1", changes)
	}

	got2, err := ReadSelection(host2, sharedScope)
	if err != nil {
		t.Fatalf("ReadSelection(host2): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"a/foo.nix"}) {
		t.Errorf("ReadSelection(host2) = %v, want untouched", got2)
	}
}

func TestRetarget_ScopedLines(t *testing.T) {
	skipIfNoNix(t)
	f := filepath.Join(t.TempDir(), "default.nix")
	mustWriteFile(t, f, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n    ../a/bar.nix\n  ];\n}\n")
	files := []SelectionFile{{Scope: config.ScopeHost, File: f}}
	if _, _, err := retarget(files, "foo", "host/new/foo.nix"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := retarget(files, "bar", "b/bar.nix"); err != nil {
		t.Fatal(err)
	}
	lines, _, _ := nix.ListImports(f)
	if want := []string{"./new/foo.nix", "../b/bar.nix"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines %v, want %v", lines, want)
	}
}

// ---- Heal ----

// healFixture lays out modules/, hosts and machines under one root, with an
// empty selection for the active host and the active machine.
func healFixture(t *testing.T) (modulesDir, hostsDir, machinesDir, machineDir string) {
	t.Helper()
	root := t.TempDir()
	modulesDir = filepath.Join(root, "modules")
	hostsDir = filepath.Join(root, ".local", "hosts")
	machinesDir = filepath.Join(root, ".local", "machines")
	machineDir = filepath.Join(machinesDir, "m1")
	mustMkdirAll(t, modulesDir)
	empty := "{ ... }:\n{\n  imports = [\n  ];\n}\n"
	mustWriteFile(t, config.SelectionPath(filepath.Join(hostsDir, "active-host")), empty)
	mustWriteFile(t, config.SelectionPath(machineDir), empty)
	return
}

func hostSelection(hostsDir, host string) string {
	return config.SelectionPath(filepath.Join(hostsDir, host))
}

func TestHeal_Move(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)
	mustWriteFile(t, filepath.Join(modulesDir, "a", "foo.nix"), "{ }")
	body := "{ ... }:\n{\n  imports = [\n    ../old/foo.nix\n  ];\n}\n"
	activeFile := hostSelection(hostsDir, "active-host")
	mustWriteFile(t, activeFile, body)
	mustWriteFile(t, hostSelection(hostsDir, "other-host"), body)

	_, changes, warnings, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host)", changes)
	}

	got, err := ReadSelection(activeFile, config.ScopeHost)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a/foo.nix"}) {
		t.Errorf("ReadSelection(active) = %v", got)
	}

	// Idempotent: second run makes no changes.
	_, changes2, warnings2, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, false)
	if err != nil {
		t.Fatalf("Heal (2nd): %v", err)
	}
	if len(changes2) != 0 || len(warnings2) != 0 {
		t.Errorf("2nd Heal: changes=%v warnings=%v, want none", changes2, warnings2)
	}
}

func TestHeal_UnresolvedActiveErrors(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)
	activeFile := hostSelection(hostsDir, "active-host")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	_, _, _, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, false)
	if err == nil {
		t.Fatalf("Heal: want error for unresolved active import")
	}

	got, err := ReadSelection(activeFile, config.ScopeHost)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"host/ghost.nix"}) {
		t.Errorf("ReadSelection(active) = %v, want untouched", got)
	}
}

func TestHeal_UnresolvedActivePruned(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)
	activeFile := hostSelection(hostsDir, "active-host")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	_, changes, _, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, true)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 1 || changes[0].New != "" {
		t.Fatalf("changes = %v, want one removal", changes)
	}

	got, err := ReadSelection(activeFile, config.ScopeHost)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadSelection(active) = %v, want empty after prune", got)
	}
}

func TestHeal_UnresolvedOtherHostSilent(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)
	otherFile := hostSelection(hostsDir, "other-host")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	moves, changes, warnings, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(moves) != 0 || len(changes) != 0 || len(warnings) != 0 {
		t.Errorf("moves=%v changes=%v warnings=%v, want none", moves, changes, warnings)
	}

	got, err := ReadSelection(otherFile, config.ScopeHost)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"host/ghost.nix"}) {
		t.Errorf("ReadSelection(other) = %v, want untouched", got)
	}
}

func TestHeal_TwoHostsIndependentHostUnits(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)

	// Both hosts select "foo" but each host's "foo" lives at a different
	// path within its own modules folder.
	mustWriteFile(t, filepath.Join(hostsDir, "host1", "modules", "a", "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostsDir, "host2", "modules", "b", "foo.nix"), "{ }")
	body := "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n"
	host1File := hostSelection(hostsDir, "host1")
	host2File := hostSelection(hostsDir, "host2")
	mustWriteFile(t, host1File, body)
	mustWriteFile(t, host2File, body)

	_, changes, warnings, err := Heal(modulesDir, hostsDir, machinesDir, "host1", machineDir, false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host, independently resolved)", changes)
	}

	got1, err := ReadSelection(host1File, config.ScopeHost)
	if err != nil || !reflect.DeepEqual(got1, []string{"host/a/foo.nix"}) {
		t.Errorf("ReadSelection(host1) = %v, %v, want [host/a/foo.nix]", got1, err)
	}
	got2, err := ReadSelection(host2File, config.ScopeHost)
	if err != nil || !reflect.DeepEqual(got2, []string{"host/b/foo.nix"}) {
		t.Errorf("ReadSelection(host2) = %v, %v, want [host/b/foo.nix]", got2, err)
	}
}

func TestHeal_MovedSharedUnitRewrittenInEveryHost(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)
	mustWriteFile(t, filepath.Join(modulesDir, "b", "shared.nix"), "{ }")
	body := "{ ... }:\n{\n  imports = [\n    ../a/shared.nix\n  ];\n}\n"
	host1File := hostSelection(hostsDir, "active-host")
	host2File := hostSelection(hostsDir, "host2")
	mustWriteFile(t, host1File, body)
	mustWriteFile(t, host2File, body)

	_, _, warnings, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	for _, f := range []string{host1File, host2File} {
		got, err := ReadSelection(f, config.ScopeHost)
		if err != nil {
			t.Fatalf("ReadSelection(%s): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/shared.nix"}) {
			t.Errorf("ReadSelection(%s) = %v, want [b/shared.nix]", f, got)
		}
	}
}

func TestHeal_HostPathOnOtherHostNotCheckedAgainstActiveFolder(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir := healFixture(t)

	// active-host has a host unit "foo.nix"; other-host does not, and its own
	// "./foo.nix" line must not be treated as existing just because the
	// active host's folder happens to have a "foo.nix".
	mustWriteFile(t, filepath.Join(hostsDir, "active-host", "modules", "foo.nix"), "{ }")
	body := "{ ... }:\n{\n  imports = [\n    ./foo.nix\n  ];\n}\n"
	mustWriteFile(t, hostSelection(hostsDir, "active-host"), body)
	otherFile := hostSelection(hostsDir, "other-host")
	mustWriteFile(t, otherFile, body)

	_, changes, warnings, err := Heal(modulesDir, hostsDir, machinesDir, "active-host", machineDir, false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none (active-host's line already exists)", changes)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none (other hosts are not reported)", warnings)
	}

	got, err := ReadSelection(otherFile, config.ScopeHost)
	if err != nil || !reflect.DeepEqual(got, []string{"host/foo.nix"}) {
		t.Errorf("ReadSelection(other) = %v, %v, want untouched", got, err)
	}
}

// ---- helpers ----

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRetarget_SkippedFileUntouched(t *testing.T) {
	skipIfNoNix(t)
	host1, host2 := setupTwoHosts(t)

	changes, _, err := retarget(sharedFiles(host2), "foo", "b/foo.nix")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host2 {
		t.Fatalf("changes = %v, want exactly one change to host2", changes)
	}
	got1, err := ReadSelection(host1, sharedScope)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got1, []string{"b/foo.nix"}) {
		t.Errorf("file outside the given set was rewritten: %v", got1)
	}
}

func bundleHosts(t *testing.T) (host1, host2 string) {
	t.Helper()
	hostsDir := t.TempDir()
	host1 = filepath.Join(hostsDir, "host1", "modules", "default.nix")
	host2 = filepath.Join(hostsDir, "host2", "modules", "default.nix")
	body := "{ ... }:\n{\n  imports = [\n    ./users/luar\n    ./users/luar/modules/git.nix\n    ./users/luar/modules/cli/zsh.nix\n    ./users/luarx/modules/git.nix\n  ];\n}\n"
	mustWriteFile(t, host1, body)
	mustWriteFile(t, host2, body)
	return
}

func TestRetargetPrefix_Rename(t *testing.T) {
	skipIfNoNix(t)
	host1, host2 := bundleHosts(t)

	if err := retargetPrefix(sharedFiles(host1, host2), "users/luar", "users/lua"); err != nil {
		t.Fatalf("retargetPrefix: %v", err)
	}
	want := []string{"users/luar", "users/lua/modules/git.nix", "users/lua/modules/cli/zsh.nix", "users/luarx/modules/git.nix"}
	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f, sharedScope)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ReadSelection(%s) = %v, want %v", f, got, want)
		}
	}
}

func TestRetargetPrefix_Remove(t *testing.T) {
	skipIfNoNix(t)
	host1, _ := bundleHosts(t)

	if err := retargetPrefix(sharedFiles(host1), "users/luar", ""); err != nil {
		t.Fatalf("retargetPrefix: %v", err)
	}
	got, err := ReadSelection(host1, sharedScope)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"users/luar", "users/luarx/modules/git.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestRetargetPrefix_OnlyGivenFiles(t *testing.T) {
	skipIfNoNix(t)
	host1, host2 := bundleHosts(t)

	if err := retargetPrefix(sharedFiles(host1), "users/luar", ""); err != nil {
		t.Fatal(err)
	}
	got2, _ := ReadSelection(host2, sharedScope)
	if len(got2) != 4 {
		t.Errorf("file outside the given set touched: %v", got2)
	}
}

func TestEnableDisable(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "a.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "b.nix"), "{ }")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	sel := config.SelectionPath(hostDir)

	if _, err := Enable(h, "missing", false); err == nil {
		t.Error("Enable of an unknown module did not fail")
	}
	if added, err := Enable(h, "a", false); err != nil || len(added) == 0 {
		t.Fatalf("Enable(a) = %v, %v; want added", added, err)
	}
	if added, err := Enable(h, "a", false); err != nil || len(added) > 0 {
		t.Errorf("second Enable(a) = %v, %v; want none (already selected)", added, err)
	}
	if added, err := Enable(h, "b", false); err != nil || len(added) == 0 {
		t.Fatalf("Enable(b) = %v, %v; want added", added, err)
	}
	got, err := ReadSelection(sel, config.ScopeHost)
	if err != nil || !reflect.DeepEqual(got, []string{"a.nix", "b.nix"}) {
		t.Errorf("selection = %v, %v", got, err)
	}
	if !reflect.DeepEqual(h.Selection(), got) {
		t.Errorf("h.Selection() = %v, want %v", h.Selection(), got)
	}

	if removed, err := Disable(h, "a"); err != nil || len(removed) != 1 {
		t.Fatalf("Disable(a) = %v, %v; want one", removed, err)
	}
	if removed, err := Disable(h, "a"); err != nil || len(removed) != 0 {
		t.Errorf("second Disable(a) = %v, %v; want none", removed, err)
	}
	got, err = ReadSelection(sel, config.ScopeHost)
	if err != nil || !reflect.DeepEqual(got, []string{"b.nix"}) || !reflect.DeepEqual(h.Selection(), got) {
		t.Errorf("selection = %v (h: %v), %v", got, h.Selection(), err)
	}
}

func TestCheckSelection(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.nix")
	mustWriteFile(t, ok, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n")
	if err := CheckSelection(ok); err != nil {
		t.Fatalf("clean selection: %v", err)
	}
	extra := filepath.Join(dir, "extra.nix")
	mustWriteFile(t, extra, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n  services.x.enable = true;\n}\n")
	err := CheckSelection(extra)
	if err == nil || !strings.Contains(err.Error(), extra) || !strings.Contains(err.Error(), "services") {
		t.Fatalf("extra attribute: got %v", err)
	}
}

func TestCheckSelectionHiddenEntry(t *testing.T) {
	skipIfNoNix(t)
	f := filepath.Join(t.TempDir(), "s.nix")
	mustWriteFile(t, f, "{ ... }:\n{\n  imports = [\n    ./a.nix ./b.nix\n  ];\n}\n")
	err := CheckSelection(f)
	if err == nil || !strings.Contains(err.Error(), "one ./path per line") {
		t.Fatalf("got %v, want the one-path-per-line error", err)
	}
}

func TestEnableRoutesByScope(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "boot.nix"), "{ }")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(h, "luar", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(h, "boot", false); err != nil {
		t.Fatal(err)
	}
	hostLines, _, _ := nix.ListImports(config.SelectionPath(hostDir))
	machineLines, _, _ := nix.ListImports(config.SelectionPath(machineDir))
	if !reflect.DeepEqual(hostLines, []string{"../users/luar.nix"}) || !reflect.DeepEqual(machineLines, []string{"./boot.nix"}) {
		t.Fatalf("host %v machine %v", hostLines, machineLines)
	}
}

func TestEnableSharedToMachine(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "devices", "laptop.nix"), "{ }")
	h, _ := Load(modulesDir, hostDir, machineDir)
	if _, err := Enable(h, "laptop", true); err != nil {
		t.Fatal(err)
	}
	machineLines, _, _ := nix.ListImports(config.SelectionPath(machineDir))
	if !reflect.DeepEqual(machineLines, []string{"../devices/laptop.nix"}) {
		t.Fatalf("machine %v", machineLines)
	}
}

func TestEnableHostUnitToMachineFails(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(hostDir, "modules", "packages.nix"), "{ }")
	h, _ := Load(modulesDir, hostDir, machineDir)
	if _, err := Enable(h, "packages", true); err == nil {
		t.Fatal("enabling a host unit into the machine selection must fail")
	}
}

func TestDisableBothSelections(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "devices", "laptop.nix"), "{ }")
	h, _ := Load(modulesDir, hostDir, machineDir)
	if _, err := Enable(h, "laptop", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(h, "laptop", true); err != nil {
		t.Fatal(err)
	}
	removed, err := Disable(h, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, []string{"laptop"}) {
		t.Fatalf("removed %v", removed)
	}
	if len(h.Selection()) != 0 {
		t.Fatalf("still selected: %v", h.Selection())
	}
}

func TestHealMovesCrossScopeLines(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	hostsDir, machinesDir := filepath.Dir(hostDir), filepath.Dir(machineDir)
	mustWriteFile(t, filepath.Join(hostDir, "modules", "packages.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "boot.nix"), "{ }")
	mustWriteFile(t, config.SelectionPath(hostDir), "{ ... }:\n{\n  imports = [\n    ../machine/boot.nix\n  ];\n}\n")
	mustWriteFile(t, config.SelectionPath(machineDir), "{ ... }:\n{\n  imports = [\n    ../host/packages.nix\n  ];\n}\n")

	moves, _, _, err := Heal(modulesDir, hostsDir, machinesDir, "h1", machineDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(moves) != 2 {
		t.Fatalf("moves %+v", moves)
	}
	hostLines, _, _ := nix.ListImports(config.SelectionPath(hostDir))
	machineLines, _, _ := nix.ListImports(config.SelectionPath(machineDir))
	if !reflect.DeepEqual(hostLines, []string{"./packages.nix"}) || !reflect.DeepEqual(machineLines, []string{"./boot.nix"}) {
		t.Fatalf("host %v machine %v", hostLines, machineLines)
	}
}

func TestHealUnresolvedOnMachineSelectionErrors(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, config.SelectionPath(machineDir), "{ ... }:\n{\n  imports = [\n    ./gone.nix\n  ];\n}\n")
	_, _, _, err := Heal(modulesDir, filepath.Dir(hostDir), filepath.Dir(machineDir), "h1", machineDir, false)
	if err == nil || !strings.Contains(err.Error(), "--prune") {
		t.Fatalf("got %v", err)
	}
	if _, changes, _, err := Heal(modulesDir, filepath.Dir(hostDir), filepath.Dir(machineDir), "h1", machineDir, true); err != nil || len(changes) != 1 {
		t.Fatalf("prune: %v %+v", err, changes)
	}
}

func TestHealRetargetsSharedMoveInEveryFolder(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	hostsDir, machinesDir := filepath.Dir(hostDir), filepath.Dir(machineDir)
	other := filepath.Join(machinesDir, "ffff")
	mustWriteFile(t, config.SelectionPath(other), "{ ... }:\n{\n  imports = [\n    ../old/laptop.nix\n  ];\n}\n")
	mustWriteFile(t, config.SelectionPath(hostDir), "{ ... }:\n{\n  imports = [\n    ../old/laptop.nix\n  ];\n}\n")
	mustWriteFile(t, filepath.Join(modulesDir, "devices", "laptop.nix"), "{ }")
	if _, _, _, err := Heal(modulesDir, hostsDir, machinesDir, "h1", machineDir, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{config.SelectionPath(other), config.SelectionPath(hostDir)} {
		lines, _, _ := nix.ListImports(f)
		if !reflect.DeepEqual(lines, []string{"../devices/laptop.nix"}) {
			t.Errorf("%s: %v", f, lines)
		}
	}
}

func userHostFixture(t *testing.T) (modulesDir, hostsDir, machinesDir, machineDir string, h *Host) {
	t.Helper()
	root := t.TempDir()
	modulesDir = filepath.Join(root, "modules")
	hostsDir = filepath.Join(root, ".local", "hosts")
	machinesDir = filepath.Join(root, ".local", "machines")
	machineDir = filepath.Join(machinesDir, "abcd")
	userFixture(t, modulesDir, "luar", []string{"a.nix", "b.nix"}, "./a.nix")
	mustWriteFile(t, filepath.Join(hostsDir, "h1", "modules", "default.nix"), selectionOf("../users/luar"))
	mustWriteFile(t, filepath.Join(machineDir, "modules", "default.nix"), selectionOf())
	var err error
	if h, err = Load(modulesDir, filepath.Join(hostsDir, "h1"), machineDir); err != nil {
		t.Fatal(err)
	}
	return
}

func TestEnable_UserSubmoduleGoesToUserSelection(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, _, _, h := userHostFixture(t)
	if _, err := Enable(h, "luar/b", false); err != nil {
		t.Fatal(err)
	}
	user := mustReadFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "default.nix"))
	if !strings.Contains(user, "./b.nix") {
		t.Errorf("user selection:\n%s", user)
	}
	if host := mustReadFile(t, hostSelection(hostsDir, "h1")); strings.Contains(host, "b.nix") {
		t.Errorf("host selection got the user's module:\n%s", host)
	}
	if _, err := Enable(h, "luar/b", true); err == nil || !strings.Contains(err.Error(), "luar") {
		t.Errorf("--machine on a user module: %v", err)
	}
}

func TestEnable_UserWithoutSelection(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "a.nix"), "{ }")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	if added, err := Enable(h, "luar/a", false); err != nil || len(added) == 0 {
		t.Fatalf("Enable = %v, %v", added, err)
	}
	file := filepath.Join(modulesDir, "users", "luar", "modules", "default.nix")
	got, err := ReadSelection(file, config.UserScope("luar"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"users/luar/modules/a.nix"}; !reflect.DeepEqual(got, want) {
		t.Errorf("selection = %v, want %v", got, want)
	}
	if sel := h.UserSelection("luar"); sel == nil || !reflect.DeepEqual(sel.Paths, got) {
		t.Errorf("loaded selection = %+v", sel)
	}
}

func TestEnable_UserUnreadableSelection(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "a.nix"), "{ }")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(modulesDir, "users", "luar", "modules", "default.nix")
	bad := "{ ... }: { }\n"
	mustWriteFile(t, file, bad)
	if _, err := Enable(h, "luar/a", false); err == nil || !strings.Contains(err.Error(), file) {
		t.Errorf("err = %v", err)
	}
	if got := mustReadFile(t, file); got != bad {
		t.Errorf("selection rewritten: %q", got)
	}
}

func TestDisable_UserSubmodule(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, _, _, _, h := userHostFixture(t)
	removed, err := Disable(h, "luar/a")
	if err != nil || len(removed) != 1 {
		t.Fatalf("Disable = %v, %v", removed, err)
	}
	if user := mustReadFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "default.nix")); strings.Contains(user, "a.nix") {
		t.Errorf("line kept:\n%s", user)
	}
}

func TestHeal_UserSelectionMovedSubmodule(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir, _ := userHostFixture(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "ui", "a.nix"), "{ }")
	if err := os.Remove(filepath.Join(modulesDir, "users", "luar", "modules", "a.nix")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Heal(modulesDir, hostsDir, machinesDir, "h1", machineDir, false); err != nil {
		t.Fatal(err)
	}
	if user := mustReadFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "default.nix")); !strings.Contains(user, "./ui/a.nix") {
		t.Errorf("user selection not healed:\n%s", user)
	}
}

func TestHeal_UserSelectionUnresolved(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir, _ := userHostFixture(t)
	userFixture(t, modulesDir, "bob", nil, "./gone.nix")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "default.nix"), selectionOf("./a.nix", "./gone.nix"))
	_, _, _, err := Heal(modulesDir, hostsDir, machinesDir, "h1", machineDir, false)
	if err == nil || !strings.Contains(err.Error(), filepath.Join("users", "luar", "modules", "default.nix")) {
		t.Fatalf("err = %v, want luar's selection named", err)
	}
	if strings.Contains(err.Error(), filepath.Join("users", "bob")) {
		t.Errorf("unselected bob reported: %v", err)
	}
}

func TestScopeOf_UserSelection(t *testing.T) {
	modulesDir, hostsDir, machinesDir, _, h := userHostFixture(t)
	userSel := filepath.Join(modulesDir, "users", "luar", "modules", "default.nix")
	has := func(s reach) bool {
		for _, f := range s.Selections {
			if f.File == userSel {
				return true
			}
		}
		return false
	}
	sub, _ := h.Find("luar/a")
	if s, err := scopeOf(hostsDir, machinesDir, h, sub); err != nil || !has(s) {
		t.Errorf("submodule scope lacks the user's selection: %+v, %v", s, err)
	}
	unit, _ := h.Find("luar")
	if s, err := scopeOf(hostsDir, machinesDir, h, unit); err != nil || has(s) {
		t.Errorf("user unit scope holds its own selection: %+v, %v", s, err)
	}
}

func TestHeal_UserSelectedByDefaultNixLine(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir, _ := userHostFixture(t)
	mustWriteFile(t, filepath.Join(hostsDir, "h1", "modules", "default.nix"), selectionOf("../users/luar/default.nix"))
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "default.nix"), selectionOf("./a.nix", "./gone.nix"))
	_, _, _, err := Heal(modulesDir, hostsDir, machinesDir, "h1", machineDir, false)
	if err == nil || !strings.Contains(err.Error(), filepath.Join("users", "luar", "modules", "default.nix")) {
		t.Fatalf("err = %v, want luar's selection named", err)
	}
}

func TestHeal_SelectedUserBrokenSelection(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostsDir, machinesDir, machineDir, _ := userHostFixture(t)
	file := filepath.Join(modulesDir, "users", "luar", "modules", "default.nix")
	mustWriteFile(t, file, "{ ... }: { }\n")
	_, _, warnings, err := Heal(modulesDir, hostsDir, machinesDir, "h1", machineDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), file) {
		t.Errorf("warnings = %v, want %s named", warnings, file)
	}
}

func TestEnableCascadesBundlesAndDisableCascadesSubmodules(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "kit", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "kit", "modules", "inner", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "kit", "modules", "inner", "modules", "leaf.nix"), "{ }")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}

	added, err := Enable(h, "kit/inner/leaf", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"kit", "kit/inner", "kit/inner/leaf"}; !reflect.DeepEqual(added, want) {
		t.Errorf("Enable added %v, want %v", added, want)
	}

	removed, err := Disable(h, "kit")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"kit/inner", "kit/inner/leaf", "kit"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("Disable removed %v, want %v", removed, want)
	}
	if len(h.Selection()) != 0 {
		t.Errorf("still selected: %v", h.Selection())
	}
}

func TestEnable_UnknownSubmoduleWritesNothing(t *testing.T) {
	skipIfNoNix(t)
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "kit", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "kit", "modules", "real.nix"), "{ }")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(hostDir, "modules", "default.nix")
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(h, "kit/ghost", false); err == nil {
		t.Fatal("Enable of an unknown submodule succeeded")
	}
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("selection changed:\n%s\nwant:\n%s", after, before)
	}
}
