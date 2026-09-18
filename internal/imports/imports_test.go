package imports

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
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

// ---- List ----

func TestList_MultiLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    ./b/c.nix # kept\n  ];\n}\n")

	got, err := List(file)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a.nix", "b/c.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestList_InlineEmpty(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [];\n}\n")

	got, err := List(file)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}

func TestList_CommentedLineIgnored(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    # ./b.nix\n  ];\n}\n")

	got, err := List(file)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}

	// The comment line itself must be untouched.
	content := mustReadFile(t, file)
	if !contains(content, "# ./b.nix") {
		t.Errorf("comment line altered: %q", content)
	}
}

// ---- Add ----

func TestAdd_MultiLine(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n")

	if err := Add(file, "./b.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := List(file)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a.nix", "b.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestAdd_InlineEmptyConverts(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [];\n}\n")

	if err := Add(file, "a.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := List(file)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a.nix"}) {
		t.Errorf("List = %v", got)
	}
}

func TestAdd_ExistingIsNoOp(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	content := "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n"
	mustWriteFile(t, file, content)

	if err := Add(file, "a.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := mustReadFile(t, file); got != content {
		t.Errorf("file changed on no-op add:\n%q\nwant\n%q", got, content)
	}
}

// ---- Remove ----

func TestRemove_MultiLine(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    ./b.nix\n  ];\n}\n")

	if err := Remove(file, "a.nix"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got, err := List(file)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"b.nix"}) {
		t.Errorf("List = %v", got)
	}
}

func TestRemove_CommentedUntouched(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    # ./b.nix\n  ];\n}\n")

	if err := Remove(file, "b.nix"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	content := mustReadFile(t, file)
	if !contains(content, "# ./b.nix") {
		t.Errorf("comment removed: %q", content)
	}
}

// ---- Refused shapes ----

func TestAdd_RefusesTwoBlocks(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n  x = {\n    imports = [\n      ./b.nix\n    ];\n  };\n}\n")

	if err := Add(file, "c.nix"); err == nil {
		t.Fatalf("Add: want error for two imports blocks")
	}
}

func TestAdd_RefusesItemOnOpeningLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [ ./a.nix\n  ];\n}\n")

	if err := Add(file, "b.nix"); err == nil {
		t.Fatalf("Add: want error for item on opening line")
	}
}

func TestList_RefusesUnrecognizedShape(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [ ./a.nix\n  ];\n}\n")

	if _, err := List(file); err == nil {
		t.Fatalf("List: want error for unrecognized shape")
	}
}

// ---- Retarget ----

func setupTwoHosts(t *testing.T) (localDir string, host1, host2 string) {
	t.Helper()
	localDir = t.TempDir()
	host1 = filepath.Join(localDir, "host1", "modules.nix")
	host2 = filepath.Join(localDir, "host2", "modules.nix")
	mustWriteFile(t, host1, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	mustWriteFile(t, host2, "{ ... }:\n{\n  imports = [\n    ./a/foo.nix\n  ];\n}\n")
	return
}

func TestRetarget_Rename(t *testing.T) {
	skipIfNoNix(t)
	localDir, host1, host2 := setupTwoHosts(t)

	changes, err := Retarget(localDir, "foo", "b/foo.nix", "")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 entries", changes)
	}

	for _, f := range []string{host1, host2} {
		got, err := List(f)
		if err != nil {
			t.Fatalf("List(%s): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/foo.nix"}) {
			t.Errorf("List(%s) = %v", f, got)
		}
	}

	// Second run: no changes (idempotent).
	changes2, err := Retarget(localDir, "foo", "b/foo.nix", "")
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_Delete(t *testing.T) {
	skipIfNoNix(t)
	localDir, host1, host2 := setupTwoHosts(t)

	changes, err := Retarget(localDir, "foo", "", "")
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
		got, err := List(f)
		if err != nil {
			t.Fatalf("List(%s): %v", f, err)
		}
		if len(got) != 0 {
			t.Errorf("List(%s) = %v, want empty", f, got)
		}
	}

	changes2, err := Retarget(localDir, "foo", "", "")
	if err != nil {
		t.Fatalf("Retarget (2nd): %v", err)
	}
	if len(changes2) != 0 {
		t.Errorf("2nd Retarget changes = %v, want none", changes2)
	}
}

func TestRetarget_HostScoped(t *testing.T) {
	skipIfNoNix(t)
	localDir, host1, host2 := setupTwoHosts(t)

	changes, err := Retarget(localDir, "foo", "b/foo.nix", "host1")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != host1 {
		t.Fatalf("changes = %v, want exactly one change to host1", changes)
	}

	got1, err := List(host1)
	if err != nil {
		t.Fatalf("List(host1): %v", err)
	}
	if !reflect.DeepEqual(got1, []string{"b/foo.nix"}) {
		t.Errorf("List(host1) = %v", got1)
	}

	got2, err := List(host2)
	if err != nil {
		t.Fatalf("List(host2): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"a/foo.nix"}) {
		t.Errorf("List(host2) = %v, want untouched", got2)
	}
}

// ---- Heal ----

func setupHealFixture(t *testing.T) (localDir, modulesDir string) {
	t.Helper()
	root := t.TempDir()
	localDir = filepath.Join(root, "local")
	modulesDir = filepath.Join(root, "modules")

	// The real unit now lives at "a/foo.nix".
	mustWriteFile(t, filepath.Join(modulesDir, "a", "foo.nix"), "{ }")

	activeFile := filepath.Join(localDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")

	other := filepath.Join(localDir, "other-host", "modules.nix")
	mustWriteFile(t, other, "{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")

	return
}

func TestHeal_Move(t *testing.T) {
	skipIfNoNix(t)
	localDir, modulesDir := setupHealFixture(t)
	activeFile := filepath.Join(localDir, "active-host", "modules.nix")

	changes, warnings, err := Heal(localDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host)", changes)
	}

	got, err := List(activeFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"a/foo.nix"}) {
		t.Errorf("List(active) = %v", got)
	}

	// Idempotent: second run makes no changes.
	changes2, warnings2, err := Heal(localDir, modulesDir, "active-host", false)
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
	localDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(localDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	_, _, err := Heal(localDir, modulesDir, "active-host", false)
	if err == nil {
		t.Fatalf("Heal: want error for unresolved active import")
	}

	// Untouched.
	got, err := List(activeFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ghost.nix"}) {
		t.Errorf("List(active) = %v, want untouched", got)
	}
}

func TestHeal_UnresolvedActivePruned(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	localDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(localDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	changes, _, err := Heal(localDir, modulesDir, "active-host", true)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 1 || changes[0].New != "" {
		t.Fatalf("changes = %v, want one removal", changes)
	}

	got, err := List(activeFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List(active) = %v, want empty after prune", got)
	}
}

func TestHeal_UnresolvedOtherHostWarns(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	localDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	activeFile := filepath.Join(localDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [];\n}\n")

	otherFile := filepath.Join(localDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./ghost.nix\n  ];\n}\n")

	changes, warnings, err := Heal(localDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none", changes)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}

	got, err := List(otherFile)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ghost.nix"}) {
		t.Errorf("List(other) = %v, want untouched", got)
	}
}

func TestHeal_TwoHostsIndependentLocalUnits(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	localDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	// Both hosts select "local/foo" but each host's "foo" lives at a
	// different path within its own local modules dir.
	mustWriteFile(t, filepath.Join(localDir, "host1", "modules", "a", "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(localDir, "host2", "modules", "b", "foo.nix"), "{ }")

	host1File := filepath.Join(localDir, "host1", "modules.nix")
	mustWriteFile(t, host1File, "{ ... }:\n{\n  imports = [\n    ./local/old/foo.nix\n  ];\n}\n")
	host2File := filepath.Join(localDir, "host2", "modules.nix")
	mustWriteFile(t, host2File, "{ ... }:\n{\n  imports = [\n    ./local/old/foo.nix\n  ];\n}\n")

	changes, warnings, err := Heal(localDir, modulesDir, "host1", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %v, want 2 (one per host, independently resolved)", changes)
	}

	got1, err := List(host1File)
	if err != nil {
		t.Fatalf("List(host1): %v", err)
	}
	if !reflect.DeepEqual(got1, []string{"local/a/foo.nix"}) {
		t.Errorf("List(host1) = %v, want [local/a/foo.nix]", got1)
	}

	got2, err := List(host2File)
	if err != nil {
		t.Fatalf("List(host2): %v", err)
	}
	if !reflect.DeepEqual(got2, []string{"local/b/foo.nix"}) {
		t.Errorf("List(host2) = %v, want [local/b/foo.nix]", got2)
	}
}

func TestHeal_MovedSharedUnitRewrittenInEveryHost(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	localDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")

	mustWriteFile(t, filepath.Join(modulesDir, "b", "shared.nix"), "{ }")

	host1File := filepath.Join(localDir, "host1", "modules.nix")
	mustWriteFile(t, host1File, "{ ... }:\n{\n  imports = [\n    ./a/shared.nix\n  ];\n}\n")
	host2File := filepath.Join(localDir, "host2", "modules.nix")
	mustWriteFile(t, host2File, "{ ... }:\n{\n  imports = [\n    ./a/shared.nix\n  ];\n}\n")

	_, warnings, err := Heal(localDir, modulesDir, "host1", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	for _, f := range []string{host1File, host2File} {
		got, err := List(f)
		if err != nil {
			t.Fatalf("List(%s): %v", f, err)
		}
		if !reflect.DeepEqual(got, []string{"b/shared.nix"}) {
			t.Errorf("List(%s) = %v, want [b/shared.nix]", f, got)
		}
	}
}

func TestHeal_LocalPathOnNonActiveHostNotCheckedAgainstActiveLocalDir(t *testing.T) {
	skipIfNoNix(t)
	root := t.TempDir()
	localDir := filepath.Join(root, "local")
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)

	// active-host has a local "foo.nix"; other-host does not, and its own
	// "./local/foo.nix" line must not be treated as existing just because
	// the active host's local dir happens to have a "foo.nix".
	mustWriteFile(t, filepath.Join(localDir, "active-host", "modules", "foo.nix"), "{ }")

	activeFile := filepath.Join(localDir, "active-host", "modules.nix")
	mustWriteFile(t, activeFile, "{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	otherFile := filepath.Join(localDir, "other-host", "modules.nix")
	mustWriteFile(t, otherFile, "{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	changes, warnings, err := Heal(localDir, modulesDir, "active-host", false)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("changes = %v, want none (active-host's line already exists)", changes)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1 (other-host's local/foo.nix does not resolve for it)", warnings)
	}

	got, err := List(otherFile)
	if err != nil {
		t.Fatalf("List(other): %v", err)
	}
	if !reflect.DeepEqual(got, []string{"local/foo.nix"}) {
		t.Errorf("List(other) = %v, want untouched", got)
	}
}

// ---- Ownership preservation ----

func TestWrite_PreservesOwnership(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	mustWriteFile(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n")

	before, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	if err := Add(file, "b.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	after, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}

	if !os.SameFile(before, after) {
		t.Errorf("file was replaced rather than written in place")
	}
}

// ---- helpers ----

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
