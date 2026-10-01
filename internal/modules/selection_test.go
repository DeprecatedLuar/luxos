package modules

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// ---- Retarget ----

func setupTwoHosts(t *testing.T) (machinesDir string, host1, host2 string) {
	t.Helper()
	machinesDir = t.TempDir()
	host1 = filepath.Join(machinesDir, "host1", "modules.nix")
	host2 = filepath.Join(machinesDir, "host2", "modules.nix")
	mustWriteFile(t, host1, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	mustWriteFile(t, host2, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	return
}

func TestRetarget_Rename(t *testing.T) {
	skipIfNoNix(t)
	machinesDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(machinesDir, "foo", "b/foo.nix", "", nil)
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 entries", changes)
	}

	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f)
		if err != nil {
			t.Fatalf("ReadSelection(%s): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/foo.nix"}) {
			t.Errorf("ReadSelection(%s) = %v", f, got)
		}
	}

	// Second run: no changes (idempotent).
	changes2, err := RetargetSelections(machinesDir, "foo", "b/foo.nix", "", nil)
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_Delete(t *testing.T) {
	skipIfNoNix(t)
	machinesDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(machinesDir, "foo", "", "", nil)
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
		got, err := ReadSelection(f)
		if err != nil {
			t.Fatalf("ReadSelection(%s): %v", f, err)
		}
		if len(got) != 0 {
			t.Errorf("ReadSelection(%s) = %v, want empty", f, got)
		}
	}

	changes2, err := RetargetSelections(machinesDir, "foo", "", "", nil)
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_HostScoped(t *testing.T) {
	skipIfNoNix(t)
	machinesDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(machinesDir, "foo", "b/foo.nix", "host1", nil)
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host1 {
		t.Fatalf("changes = %v, want exactly one change to host1", changes)
	}

	got1, err := ReadSelection(host1)
	if err != nil {
		t.Fatalf("ReadSelection(host1): %v", err)
	}
	if !reflect.DeepEqual(got1, []string{"b/foo.nix"}) {
		t.Errorf("ReadSelection(host1) = %v", got1)
	}

	got2, err := ReadSelection(host2)
	if err != nil {
		t.Fatalf("ReadSelection(host2): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"a/foo.nix"}) {
		t.Errorf("ReadSelection(host2) = %v, want untouched", got2)
	}
}

// ---- Heal ----

func setupHealFixture(t *testing.T) (machinesDir, modulesDir string) {
	t.Helper()
	root := t.TempDir()
	machinesDir = filepath.Join(root, "local")
	modulesDir = filepath.Join(root, "modules")

	// The real unit now lives at "a/foo.nix".
	mustWriteFile(t, filepath.Join(modulesDir, "a", "foo.nix"), "{ }")

	activeFile := filepath.Join(machinesDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")

	other := filepath.Join(machinesDir, "other-host", "modules.nix")
	mustWriteFile(t, other, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")

	return
}

func TestHeal_Move(t *testing.T) {
	skipIfNoNix(t)
	machinesDir, modulesDir := setupHealFixture(t)
	activeFile := filepath.Join(machinesDir, "active-host", "modules.nix")

	changes, warnings, err := Heal(machinesDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host)", changes)
	}

	got, err := ReadSelection(activeFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a/foo.nix"}) {
		t.Errorf("ReadSelection(active) = %v", got)
	}

	// Idempotent: second run makes no changes.
	changes2, warnings2, err := Heal(machinesDir, modulesDir, "active-host", false)
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
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(machinesDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	_, _, err := Heal(machinesDir, modulesDir, "active-host", false)
	if err == nil {
		t.Fatalf("Heal: want error for unresolved active import")
	}

	// Untouched.
	got, err := ReadSelection(activeFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ghost.nix"}) {
		t.Errorf("ReadSelection(active) = %v, want untouched", got)
	}
}

func TestHeal_UnresolvedActivePruned(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(machinesDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	changes, _, err := Heal(machinesDir, modulesDir, "active-host", true)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 1 || changes[0].New != "" {
		t.Fatalf("changes = %v, want one removal", changes)
	}

	got, err := ReadSelection(activeFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ReadSelection(active) = %v, want empty after prune", got)
	}
}

func TestHeal_UnresolvedOtherHostWarns(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(machinesDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [];\n}\n")

	otherFile := filepath.Join(machinesDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	changes, warnings, err := Heal(machinesDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none", changes)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}

	got, err := ReadSelection(otherFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ghost.nix"}) {
		t.Errorf("ReadSelection(other) = %v, want untouched", got)
	}
}

func TestHeal_TwoHostsIndependentLocalUnits(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	// Both hosts select "local/foo" but each host's "foo" lives at a
	// different path within its own local modules dir.
	mustWriteFile(t, filepath.Join(machinesDir, "host1", "modules", "a", "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machinesDir, "host2", "modules", "b", "foo.nix"), "{ }")

	host1File := filepath.Join(machinesDir, "host1", "modules.nix")
	mustWriteFile(t, host1File, "{ ... }:\n{\n  imports = [\n    ./local/old/foo.nix\n  ];\n}\n")
	host2File := filepath.Join(machinesDir, "host2", "modules.nix")
	mustWriteFile(t, host2File, "{ ... }:\n{\n  imports = [\n    ./local/old/foo.nix\n  ];\n}\n")

	changes, warnings, err := Heal(machinesDir, modulesDir, "host1", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host, independently resolved)", changes)
	}

	got1, err := ReadSelection(host1File)
	if err != nil {
		t.Fatalf("ReadSelection(host1): %v", err)
	}
	if !reflect.DeepEqual(got1, []string{"local/a/foo.nix"}) {
		t.Errorf("ReadSelection(host1) = %v, want [local/a/foo.nix]", got1)
	}

	got2, err := ReadSelection(host2File)
	if err != nil {
		t.Fatalf("ReadSelection(host2): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"local/b/foo.nix"}) {
		t.Errorf("ReadSelection(host2) = %v, want [local/b/foo.nix]", got2)
	}
}

func TestHeal_MovedSharedUnitRewrittenInEveryHost(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")

	mustWriteFile(t, filepath.Join(modulesDir, "b", "shared.nix"), "{ }")

	host1File := filepath.Join(machinesDir, "host1", "modules.nix")
	mustWriteFile(t, host1File, "{ ... }:\n{\n  imports = [\n    ./a/shared.nix\n  ];\n}\n")
	host2File := filepath.Join(machinesDir, "host2", "modules.nix")
	mustWriteFile(t, host2File, "{ ... }:\n{\n  imports = [\n    ./a/shared.nix\n  ];\n}\n")

	_, warnings, err := Heal(machinesDir, modulesDir, "host1", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	for _, f := range []string{host1File, host2File} {
		got, err := ReadSelection(f)
		if err != nil {
			t.Fatalf("ReadSelection(%s): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/shared.nix"}) {
			t.Errorf("ReadSelection(%s) = %v, want [b/shared.nix]", f, got)
		}
	}
}

func TestHeal_LocalPathOnNonActiveHostNotCheckedAgainstActiveLocalDir(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	// active-host has a local "foo.nix"; other-host does not, and its own
	// "./local/foo.nix" line must not be treated as existing just because
	// the active host's local dir happens to have a "foo.nix".
	mustWriteFile(t, filepath.Join(machinesDir, "active-host", "modules", "foo.nix"), "{ }")

	activeFile := filepath.Join(machinesDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	otherFile := filepath.Join(machinesDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	changes, warnings, err := Heal(machinesDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none (active-host's line already exists)", changes)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1 (other-host's local/foo.nix does not resolve for it)", warnings)
	}

	got, err := ReadSelection(otherFile)
	if err != nil {
		t.Fatalf("ReadSelection(other): %v", err)
	}
	if !reflect.DeepEqual(got, []string{"local/foo.nix"}) {
		t.Errorf("ReadSelection(other) = %v, want untouched", got)
	}
}

// ---- helpers ----

func TestHeal_HardwareUnitSkippedOnOtherHost(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	machinesDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)
	content := "{ ... }:\n{\n  imports = [\n    ./local/hardware-support\n  ];\n}\n"
	mustWriteFile(t, filepath.Join(machinesDir, "active-host", "modules.nix"), "{ ... }:\n{\n  imports = [\n  ];\n}\n")
	otherFile := filepath.Join(machinesDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, content)

	changes, warnings, err := Heal(machinesDir, modulesDir, "active-host", false)
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
	machinesDir, host1, host2 := setupTwoHosts(t)

	changes, err := RetargetSelections(machinesDir, "foo", "b/foo.nix", "", []string{"host1"})
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host2 {
		t.Fatalf("changes = %v, want exactly one change to host2", changes)
	}
	got1, err := ReadSelection(host1)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(got1, []string{"b/foo.nix"}) {
		t.Errorf("skipped host was rewritten: %v", got1)
	}
}

func bundleHosts(t *testing.T) (machinesDir, host1, host2 string) {
	t.Helper()
	machinesDir = t.TempDir()
	host1 = filepath.Join(machinesDir, "host1", "modules.nix")
	host2 = filepath.Join(machinesDir, "host2", "modules.nix")
	body := "{ ... }:\n{\n  imports = [\n    ./users/luar\n    ./users/luar/modules/git.nix\n    ./users/luar/modules/cli/zsh.nix\n    ./users/luarx/modules/git.nix\n  ];\n}\n"
	mustWriteFile(t, host1, body)
	mustWriteFile(t, host2, body)
	return
}

func TestRetargetPrefix_Rename(t *testing.T) {
	skipIfNoNix(t)
	machinesDir, host1, host2 := bundleHosts(t)

	changes, err := RetargetPrefix(machinesDir, "users/luar", "users/lua", "", nil)
	if err != nil {
		t.Fatalf("RetargetPrefix: %v", err)
	}
	if len(changes) != 4 {
		t.Fatalf("changes = %v, want 4 (two lines in each host)", changes)
	}
	want := []string{"users/luar", "users/lua/modules/git.nix", "users/lua/modules/cli/zsh.nix", "users/luarx/modules/git.nix"}
	for _, f := range []string{host1, host2} {
		got, err := ReadSelection(f)
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
	machinesDir, host1, _ := bundleHosts(t)

	if _, err := RetargetPrefix(machinesDir, "users/luar", "", "", nil); err != nil {
		t.Fatalf("RetargetPrefix: %v", err)
	}
	got, err := ReadSelection(host1)
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
	machinesDir, host1, host2 := bundleHosts(t)

	if _, err := RetargetPrefix(machinesDir, "users/luar", "", "host1", nil); err != nil {
		t.Fatal(err)
	}
	got2, _ := ReadSelection(host2)
	if len(got2) != 4 {
		t.Errorf("host2 touched by host-scoped call: %v", got2)
	}

	machinesDir, host1, host2 = bundleHosts(t)
	if _, err := RetargetPrefix(machinesDir, "users/luar", "", "", []string{"host2"}); err != nil {
		t.Fatal(err)
	}
	got1, _ := ReadSelection(host1)
	got2, _ = ReadSelection(host2)
	if len(got1) != 2 || len(got2) != 4 {
		t.Errorf("host1 = %v, host2 = %v", got1, got2)
	}
}
