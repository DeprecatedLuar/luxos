package units

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func unitByName(t *testing.T, us []Unit, name string) Unit {
	t.Helper()
	for _, u := range us {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("no unit named %q in %+v", name, us)
	return Unit{}
}

func TestWalk_FileUnit(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")

	us, err := Walk(root, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	u := unitByName(t, us, "hyprland")
	if u.Path != "hyprland.nix" {
		t.Errorf("Path = %q, want hyprland.nix", u.Path)
	}
}

func TestWalk_FolderUnitNotDescended(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "wayland", "default.nix"), "{ }")
	// A file inside the unit's own folder must not surface as a separate
	// unit — the walk must not descend into it.
	mustWriteFile(t, filepath.Join(root, "wayland", "extra.nix"), "{ }")

	us, err := Walk(root, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 1 {
		t.Fatalf("got %d units, want 1: %+v", len(us), us)
	}
	u := unitByName(t, us, "wayland")
	if u.Path != "wayland" {
		t.Errorf("Path = %q, want wayland", u.Path)
	}
}

func TestWalk_CategoryDescent(t *testing.T) {
	root := t.TempDir()
	// "services" has no default.nix of its own: transparent category, so
	// its children surface as top-level units by name.
	mustWriteFile(t, filepath.Join(root, "services", "sshd.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "services", "nested", "default.nix"), "{ }")

	us, err := Walk(root, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sshd := unitByName(t, us, "sshd")
	if sshd.Path != filepath.Join("services", "sshd.nix") {
		t.Errorf("sshd Path = %q", sshd.Path)
	}
	nested := unitByName(t, us, "nested")
	if nested.Path != filepath.Join("services", "nested") {
		t.Errorf("nested Path = %q", nested.Path)
	}
}

func TestWalk_RootDefaultNixSkipped(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")

	us, err := Walk(root, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 1 {
		t.Fatalf("got %d units, want 1 (root default.nix must be skipped): %+v", len(us), us)
	}
	if us[0].Name != "hyprland" {
		t.Errorf("Name = %q, want hyprland", us[0].Name)
	}
}

func TestWalk_SymlinkedDirectoryFollowed(t *testing.T) {
	root := t.TempDir()
	realDir := t.TempDir()
	mustWriteFile(t, filepath.Join(realDir, "default.nix"), "{ }")

	link := filepath.Join(root, "linked")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	us, err := Walk(root, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	u := unitByName(t, us, "linked")
	if u.Path != "linked" {
		t.Errorf("Path = %q, want linked", u.Path)
	}
}

func TestWalk_DuplicateAcrossCategories(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a", "shared.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "b", "shared.nix"), "{ }")

	_, err := Walk(root, "")
	if err == nil {
		t.Fatalf("expected error for duplicate name")
	}
	msg := err.Error()
	if !strings.Contains(msg, "duplicate module name 'shared':") {
		t.Errorf("error missing header, got: %s", msg)
	}
	if !strings.Contains(msg, "  - "+filepath.Join("a", "shared.nix")) {
		t.Errorf("error missing claimant a/shared.nix, got: %s", msg)
	}
	if !strings.Contains(msg, "  - "+filepath.Join("b", "shared.nix")) {
		t.Errorf("error missing claimant b/shared.nix, got: %s", msg)
	}
}

func TestWalk_SortedByName(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "zeta.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "alpha.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "mid.nix"), "{ }")

	us, err := Walk(root, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var names []string
	for _, u := range us {
		names = append(names, u.Name)
	}
	want := []string{"alpha", "mid", "zeta"}
	if len(names) != len(want) {
		t.Fatalf("got %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestWalk_LocalUnitsPrefixed(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "cat", "bar.nix"), "{ }")

	us, err := Walk(root, local)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 3 {
		t.Fatalf("got %d units, want 3: %+v", len(us), us)
	}
	foo := unitByName(t, us, "foo")
	if foo.Path != "local/foo.nix" {
		t.Errorf("foo Path = %q, want local/foo.nix", foo.Path)
	}
	bar := unitByName(t, us, "bar")
	if bar.Path != filepath.Join("local", "cat", "bar.nix") {
		t.Errorf("bar Path = %q, want local/cat/bar.nix", bar.Path)
	}
	hyprland := unitByName(t, us, "hyprland")
	if hyprland.Path != "hyprland.nix" {
		t.Errorf("hyprland Path = %q, want hyprland.nix", hyprland.Path)
	}
}

func TestWalk_MissingLocalDirIsFine(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")
	missingLocal := filepath.Join(t.TempDir(), "does-not-exist")

	us, err := Walk(root, missingLocal)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 1 {
		t.Fatalf("got %d units, want 1: %+v", len(us), us)
	}
}

func TestWalk_SameNameSharedAndLocalIsDuplicate(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "foo.nix"), "{ }")

	_, err := Walk(root, local)
	if err == nil {
		t.Fatalf("expected error for duplicate name across shared+local")
	}
	msg := err.Error()
	if !strings.Contains(msg, "duplicate module name 'foo':") {
		t.Errorf("error missing header, got: %s", msg)
	}
	if !strings.Contains(msg, "  - foo.nix") {
		t.Errorf("error missing shared claimant, got: %s", msg)
	}
	if !strings.Contains(msg, "  - local/foo.nix") {
		t.Errorf("error missing local claimant, got: %s", msg)
	}
}

func TestWalk_RootLocalNeverWalked(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	// A real "local" directory sitting at modulesDir's own root (as the
	// mirror link's target would be) must never be descended into here,
	// even though it isn't a symlink in this fixture.
	mustWriteFile(t, filepath.Join(root, "local", "should-not-appear.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")

	us, err := Walk(root, local)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 1 {
		t.Fatalf("got %d units, want 1 (root local/ must never be walked): %+v", len(us), us)
	}
	if us[0].Name != "hyprland" {
		t.Errorf("Name = %q, want hyprland", us[0].Name)
	}
}

func TestResolve(t *testing.T) {
	us := []Unit{
		{Name: "hyprland", Path: "hyprland.nix"},
		{Name: "wayland", Path: "wayland"},
	}

	path, ok := Resolve(us, "wayland")
	if !ok || path != "wayland" {
		t.Errorf("Resolve(wayland) = %q, %v", path, ok)
	}

	_, ok = Resolve(us, "missing")
	if ok {
		t.Errorf("Resolve(missing) = true, want false")
	}
}

func TestNameFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"a/b/hyprland.nix", "hyprland"},
		{"a/b/hyprland", "hyprland"},
		{"a/b/hyprland/default.nix", "hyprland"},
	}
	for _, c := range cases {
		got := NameFromPath(c.path)
		if got != c.want {
			t.Errorf("NameFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
