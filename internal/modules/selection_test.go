package modules

import (
	"github.com/DeprecatedLuar/luxos/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---- Retarget ----

func setupTwoHosts(t *testing.T) (hostsDir string, host1, host2 string) {
	t.Helper()
	hostsDir = t.TempDir()
	host1 = filepath.Join(hostsDir, "host1", "modules.nix")
	host2 = filepath.Join(hostsDir, "host2", "modules.nix")
	mustWriteFile(t, host1, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	mustWriteFile(t, host2, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	return
}

func TestRetarget_Rename(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(hostsDir, "foo", "b/foo.nix", "", nil)
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 entries", changes)
	}

	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f, config.ScopeShared)
		if err != nil {
			t.Fatalf("ReadSelection(%s, config.ScopeShared): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/foo.nix"}) {
			t.Errorf("ReadSelection(%s, config.ScopeShared) = %v", f, got)
		}
	}

	// Second run: no changes (idempotent).
	changes2, err := RetargetSelections(hostsDir, "foo", "b/foo.nix", "", nil)
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_Delete(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(hostsDir, "foo", "", "", nil)
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
		got, err := ReadSelection(f, config.ScopeShared)
		if err != nil {
			t.Fatalf("ReadSelection(%s, config.ScopeShared): %v", f, err)
		}
		if len(got) != 0 {
			t.Errorf("ReadSelection(%s, config.ScopeShared) = %v, want empty", f, got)
		}
	}

	changes2, err := RetargetSelections(hostsDir, "foo", "", "", nil)
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_HostScoped(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(hostsDir, "foo", "b/foo.nix", "host1", nil)
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host1 {
		t.Fatalf("changes = %v, want exactly one change to host1", changes)
	}

	got1, err := ReadSelection(host1, config.ScopeShared)
	if err != nil {
		t.Fatalf("ReadSelection(host1, config.ScopeShared): %v", err)
	}
	if !reflect.DeepEqual(got1, []string{"b/foo.nix"}) {
		t.Errorf("ReadSelection(host1, config.ScopeShared) = %v", got1)
	}

	got2, err := ReadSelection(host2, config.ScopeShared)
	if err != nil {
		t.Fatalf("ReadSelection(host2, config.ScopeShared): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"a/foo.nix"}) {
		t.Errorf("ReadSelection(host2, config.ScopeShared) = %v, want untouched", got2)
	}
}

// ---- Heal ----

func setupHealFixture(t *testing.T) (hostsDir, modulesDir string) {
	t.Helper()
	root := t.TempDir()
	hostsDir = filepath.Join(root, "local")
	modulesDir = filepath.Join(root, "modules")

	// The real unit now lives at "a/foo.nix".
	mustWriteFile(t, filepath.Join(modulesDir, "a", "foo.nix"), "{ }")

	activeFile := filepath.Join(hostsDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")

	other := filepath.Join(hostsDir, "other-host", "modules.nix")
	mustWriteFile(t, other, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")

	return
}

func TestHeal_Move(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, modulesDir := setupHealFixture(t)
	activeFile := filepath.Join(hostsDir, "active-host", "modules.nix")

	changes, warnings, err := Heal(hostsDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host)", changes)
	}

	got, err := ReadSelection(activeFile, config.ScopeShared)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a/foo.nix"}) {
		t.Errorf("ReadSelection(active, config.ScopeShared) = %v", got)
	}

	// Idempotent: second run makes no changes.
	changes2, warnings2, err := Heal(hostsDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal (2nd): %v", err)
	}
	if len(changes2) != 0 || len(warnings2) != 0 {
		t.Errorf("2nd Heal: changes=%v warnings=%v, want none", changes2, warnings2)
	}
}

func TestHeal_UnresolvedActiveErrors(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(hostsDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	_, _, err := Heal(hostsDir, modulesDir, "active-host", false)
	if err == nil {
		t.Fatalf("Heal: want error for unresolved active import")
	}

	// Untouched.
	got, err := ReadSelection(activeFile, config.ScopeShared)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ghost.nix"}) {
		t.Errorf("ReadSelection(active, config.ScopeShared) = %v, want untouched", got)
	}
}

func TestHeal_UnresolvedActivePruned(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(hostsDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	changes, _, err := Heal(hostsDir, modulesDir, "active-host", true)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 1 || changes[0].New != "" {
		t.Fatalf("changes = %v, want one removal", changes)
	}

	got, err := ReadSelection(activeFile, config.ScopeShared)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadSelection(active, config.ScopeShared) = %v, want empty after prune", got)
	}
}

func TestHeal_UnresolvedOtherHostSilent(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(hostsDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [];\n}\n")

	otherFile := filepath.Join(hostsDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	changes, warnings, err := Heal(hostsDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none", changes)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	got, err := ReadSelection(otherFile, config.ScopeShared)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ghost.nix"}) {
		t.Errorf("ReadSelection(other, config.ScopeShared) = %v, want untouched", got)
	}
}

func TestHeal_TwoHostsIndependentLocalUnits(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	// Both hosts select "local/foo" but each host's "foo" lives at a
	// different path within its own local modules dir.
	mustWriteFile(t, filepath.Join(hostsDir, "host1", "modules", "a", "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostsDir, "host2", "modules", "b", "foo.nix"), "{ }")

	host1File := filepath.Join(hostsDir, "host1", "modules.nix")
	mustWriteFile(t, host1File, "{ ... }:\n{\n  imports = [\n    ./local/old/foo.nix\n  ];\n}\n")
	host2File := filepath.Join(hostsDir, "host2", "modules.nix")
	mustWriteFile(t, host2File, "{ ... }:\n{\n  imports = [\n    ./local/old/foo.nix\n  ];\n}\n")

	changes, warnings, err := Heal(hostsDir, modulesDir, "host1", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host, independently resolved)", changes)
	}

	got1, err := ReadSelection(host1File, config.ScopeShared)
	if err != nil {
		t.Fatalf("ReadSelection(host1, config.ScopeShared): %v", err)
	}
	if !reflect.DeepEqual(got1, []string{"local/a/foo.nix"}) {
		t.Errorf("ReadSelection(host1, config.ScopeShared) = %v, want [local/a/foo.nix]", got1)
	}

	got2, err := ReadSelection(host2File, config.ScopeShared)
	if err != nil {
		t.Fatalf("ReadSelection(host2, config.ScopeShared): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"local/b/foo.nix"}) {
		t.Errorf("ReadSelection(host2, config.ScopeShared) = %v, want [local/b/foo.nix]", got2)
	}
}

func TestHeal_MovedSharedUnitRewrittenInEveryHost(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")

	mustWriteFile(t, filepath.Join(modulesDir, "b", "shared.nix"), "{ }")

	host1File := filepath.Join(hostsDir, "host1", "modules.nix")
	mustWriteFile(t, host1File, "{ ... }:\n{\n  imports = [\n    ./a/shared.nix\n  ];\n}\n")
	host2File := filepath.Join(hostsDir, "host2", "modules.nix")
	mustWriteFile(t, host2File, "{ ... }:\n{\n  imports = [\n    ./a/shared.nix\n  ];\n}\n")

	_, warnings, err := Heal(hostsDir, modulesDir, "host1", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	for _, f := range []string{host1File, host2File} {
		got, err := ReadSelection(f, config.ScopeShared)
		if err != nil {
			t.Fatalf("ReadSelection(%s, config.ScopeShared): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/shared.nix"}) {
			t.Errorf("ReadSelection(%s, config.ScopeShared) = %v, want [b/shared.nix]", f, got)
		}
	}
}

func TestHeal_LocalPathOnNonActiveHostNotCheckedAgainstActiveLocalDir(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	// active-host has a local "foo.nix"; other-host does not, and its own
	// "./local/foo.nix" line must not be treated as existing just because
	// the active host's local dir happens to have a "foo.nix".
	mustWriteFile(t, filepath.Join(hostsDir, "active-host", "modules", "foo.nix"), "{ }")

	activeFile := filepath.Join(hostsDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	otherFile := filepath.Join(hostsDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	changes, warnings, err := Heal(hostsDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none (active-host's line already exists)", changes)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none (other hosts are not reported)", warnings)
	}

	got, err := ReadSelection(otherFile, config.ScopeShared)
	if err != nil {
		t.Fatalf("ReadSelection(other, config.ScopeShared): %v", err)
	}
	if !reflect.DeepEqual(got, []string{"local/foo.nix"}) {
		t.Errorf("ReadSelection(other, config.ScopeShared) = %v, want untouched", got)
	}
}

// ---- helpers ----

func TestHeal_HardwareUnitSkippedOnOtherHost(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	hostsDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)
	content := "{ ... }:\n{\n  imports = [\n    ./local/hardware-support\n  ];\n}\n"
	mustWriteFile(t, filepath.Join(hostsDir, "active-host", "modules.nix"), "{ ... }:\n{\n  imports = [\n  ];\n}\n")
	otherFile := filepath.Join(hostsDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, content)

	changes, warnings, err := Heal(hostsDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 || len(warnings) != 0 {
		t.Errorf("changes=%v warnings=%v, want none", changes, warnings)
	}
	if got := mustReadFile(t, otherFile); got != content {
		t.Errorf("other host file changed: %q", got)
	}
}

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

func TestRetarget_SkipHosts(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(hostsDir, "foo", "b/foo.nix", "", []string{"host1"})
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host2 {
		t.Fatalf("changes = %v, want exactly one change to host2", changes)
	}
	got1, err := ReadSelection(host1, config.ScopeShared)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got1, []string{"b/foo.nix"}) {
		t.Errorf("skipped host was rewritten: %v", got1)
	}
}

func bundleHosts(t *testing.T) (hostsDir, host1, host2 string) {
	t.Helper()
	hostsDir = t.TempDir()
	host1 = filepath.Join(hostsDir, "host1", "modules.nix")
	host2 = filepath.Join(hostsDir, "host2", "modules.nix")
	body := "{ ... }:\n{\n  imports = [\n    ./users/luar\n    ./users/luar/modules/git.nix\n    ./users/luar/modules/cli/zsh.nix\n    ./users/luarx/modules/git.nix\n  ];\n}\n"
	mustWriteFile(t, host1, body)
	mustWriteFile(t, host2, body)
	return
}

func TestRetargetPrefix_Rename(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, host1, host2 := bundleHosts(t)

	changes, err := RetargetPrefix(hostsDir, "users/luar", "users/lua", "", nil)
	if err != nil {
		t.Fatalf("RetargetPrefix: %v", err)
	}
	if len(changes) != 4 {
		t.Fatalf("changes = %v, want 4 (two lines in each host)", changes)
	}
	want := []string{"users/luar", "users/lua/modules/git.nix", "users/lua/modules/cli/zsh.nix", "users/luarx/modules/git.nix"}
	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f, config.ScopeShared)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ReadSelection(%s, config.ScopeShared) = %v, want %v", f, got, want)
		}
	}
}

func TestRetargetPrefix_Remove(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, host1, _ := bundleHosts(t)

	if _, err := RetargetPrefix(hostsDir, "users/luar", "", "", nil); err != nil {
		t.Fatalf("RetargetPrefix: %v", err)
	}
	got, err := ReadSelection(host1, config.ScopeShared)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"users/luar", "users/luarx/modules/git.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestRetargetPrefix_HostScopeAndSkip(t *testing.T) {
	skipIfNoNix(t)
	hostsDir, _, host2 := bundleHosts(t)

	if _, err := RetargetPrefix(hostsDir, "users/luar", "", "host1", nil); err != nil {
		t.Fatal(err)
	}
	got2, _ := ReadSelection(host2, config.ScopeShared)
	if len(got2) != 4 {
		t.Errorf("host2 touched by host-scoped call: %v", got2)
	}

	hostsDir, host1, host2 := bundleHosts(t)
	if _, err := RetargetPrefix(hostsDir, "users/luar", "", "", []string{"host2"}); err != nil {
		t.Fatal(err)
	}
	got1, _ := ReadSelection(host1, config.ScopeShared)
	got2, _ = ReadSelection(host2, config.ScopeShared)
	if len(got1) != 2 || len(got2) != 4 {
		t.Errorf("host1 = %v, host2 = %v", got1, got2)
	}
}

func TestEnableDisable(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, []string{"a.nix"}, "a.nix", "b.nix")
	sel := filepath.Join(h.HostDir, SelectionFile)

	if _, err := Enable(h, "missing"); err == nil {
		t.Error("Enable of an unknown module did not fail")
	}
	if changed, err := Enable(h, "a"); err != nil || changed {
		t.Errorf("Enable(a) = %v, %v; want false (already selected)", changed, err)
	}
	if changed, err := Enable(h, "b"); err != nil || !changed {
		t.Fatalf("Enable(b) = %v, %v; want true", changed, err)
	}
	got, err := ReadSelection(sel, config.ScopeShared)
	if err != nil || !reflect.DeepEqual(got, []string{"a.nix", "b.nix"}) {
		t.Errorf("selection = %v, %v", got, err)
	}
	if !reflect.DeepEqual(h.Selection, got) {
		t.Errorf("h.Selection = %v, want %v", h.Selection, got)
	}

	if changed, err := Disable(h, "a"); err != nil || !changed {
		t.Fatalf("Disable(a) = %v, %v; want true", changed, err)
	}
	if changed, err := Disable(h, "a"); err != nil || changed {
		t.Errorf("second Disable(a) = %v, %v; want false", changed, err)
	}
	got, err = ReadSelection(sel, config.ScopeShared)
	if err != nil || !reflect.DeepEqual(got, []string{"b.nix"}) || !reflect.DeepEqual(h.Selection, got) {
		t.Errorf("selection = %v (h: %v), %v", got, h.Selection, err)
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
