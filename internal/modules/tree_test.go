package modules

import (
	"github.com/DeprecatedLuar/luxos/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

func unitByName(t *testing.T, us []Module, name string) Module {
	t.Helper()
	for _, u := range us {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("no unit named %q in %+v", name, us)
	return Module{}
}

func TestWalk_FileUnit(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")

	us, _, err := Walk(root, "", "")
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

	us, _, err := Walk(root, "", "")
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

	us, _, err := Walk(root, "", "")
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

	us, _, err := Walk(root, "", "")
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

	us, _, err := Walk(root, "", "")
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

	_, _, err := Walk(root, "", "")
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

	us, _, err := Walk(root, "", "")
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

func TestWalk_HostUnitsPrefixed(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "cat", "bar.nix"), "{ }")

	us, _, err := Walk(root, local, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 3 {
		t.Fatalf("got %d units, want 3: %+v", len(us), us)
	}
	foo := unitByName(t, us, "foo")
	if foo.Path != "host/foo.nix" {
		t.Errorf("foo Path = %q, want host/foo.nix", foo.Path)
	}
	bar := unitByName(t, us, "bar")
	if bar.Path != filepath.Join("host", "cat", "bar.nix") {
		t.Errorf("bar Path = %q, want host/cat/bar.nix", bar.Path)
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

	us, _, err := Walk(root, missingLocal, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 1 {
		t.Fatalf("got %d units, want 1: %+v", len(us), us)
	}
}

func TestWalk_LocalFileShadowsSharedFolder(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "desktop", "ambxst", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "ambxst.nix"), "{ }")

	us, _, err := Walk(root, local, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Module{{Name: "ambxst", Path: "host/ambxst.nix", Shadows: "desktop/ambxst", Abs: filepath.Join(local, "ambxst.nix"), Scope: config.ScopeHost}}
	if !reflect.DeepEqual(us, want) {
		t.Errorf("got %+v, want %+v", us, want)
	}
}

func TestWalk_LocalFolderShadowsSharedFile(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "cat", "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "foo", "default.nix"), "{ }")

	us, _, err := Walk(root, local, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Module{{Name: "foo", Path: "host/foo", Shadows: "cat/foo.nix", Abs: filepath.Join(local, "foo"), Scope: config.ScopeHost}}
	if !reflect.DeepEqual(us, want) {
		t.Errorf("got %+v, want %+v", us, want)
	}
}

func TestWalk_LocalShadowsSystemUnit(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "system", "audio.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "audio.nix"), "{ }")

	us, _, err := Walk(root, local, "")
	if err != nil {
		t.Fatal(err)
	}
	u, ok := Find(us, "audio")
	if !ok || u.Path != "host/audio.nix" || u.Shadows != "system/audio.nix" || len(us) != 1 {
		t.Errorf("got %+v", us)
	}
}

func TestWalk_LocalLocalDuplicateIsError(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(local, "a", "foo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(local, "b", "foo.nix"), "{ }")

	_, _, err := Walk(root, local, "")
	if err == nil || !strings.Contains(err.Error(), "duplicate module name 'foo':") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestFind(t *testing.T) {
	us := []Module{{Name: "a", Path: "a.nix"}}
	if u, ok := Find(us, "a"); !ok || u.Path != "a.nix" {
		t.Errorf("Find(a) = %+v, %v", u, ok)
	}
	if _, ok := Find(us, "b"); ok {
		t.Error("Find(b) should miss")
	}
}

func TestWalk_ReservedRootEntriesNeverWalked(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	// Real directories named like the scope links, sitting at modulesDir's
	// own root, must never be descended into.
	mustWriteFile(t, filepath.Join(root, "host", "should-not-appear.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "machine", "should-not-appear.nix"), "{ }")
	mustWriteFile(t, filepath.Join(root, "hyprland.nix"), "{ }")

	us, _, err := Walk(root, local, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(us) != 1 {
		t.Fatalf("got %d units, want 1 (root host/ and machine/ must never be walked): %+v", len(us), us)
	}
	if us[0].Name != "hyprland" {
		t.Errorf("Name = %q, want hyprland", us[0].Name)
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

func TestNameFromPath_Bundles(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"apps/eduardo/modules/git.nix", "eduardo/git"},
		{"apps/eduardo/modules/cli/git.nix", "eduardo/git"},
		{"a/eduardo/modules/nvim/modules/lsp/default.nix", "eduardo/nvim/lsp"},
		{"host/eduardo/modules/git.nix", "eduardo/git"},
		{"desktop/hyprland.nix", "hyprland"},
		{"modules/foo.nix", "foo"},
		{"apps/eduardo/modules/git/default.nix", "eduardo/git"},
	}
	for _, c := range cases {
		if got := NameFromPath(c.path); got != c.want {
			t.Errorf("NameFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestParentAndTop(t *testing.T) {
	cases := []struct{ name, parent, top string }{
		{"hyprland", "", "hyprland"},
		{"eduardo/git", "eduardo", "eduardo"},
		{"eduardo/nvim/lsp", "eduardo/nvim", "eduardo"},
	}
	for _, c := range cases {
		if got := Parent(c.name); got != c.parent {
			t.Errorf("Parent(%q) = %q, want %q", c.name, got, c.parent)
		}
		if got := Top(c.name); got != c.top {
			t.Errorf("Top(%q) = %q, want %q", c.name, got, c.top)
		}
	}
}

func TestIsBundle(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "bundle", "default.nix"), "{ }")
	mustMkdirAll(t, filepath.Join(root, "bundle", "modules"))
	mustWriteFile(t, filepath.Join(root, "plain", "default.nix"), "{ }")
	mustMkdirAll(t, filepath.Join(root, "nodefault", "modules"))
	for dir, want := range map[string]bool{"bundle": true, "plain": false, "nodefault": false, "missing": false} {
		if got := IsBundle(filepath.Join(root, dir)); got != want {
			t.Errorf("IsBundle(%s) = %v, want %v", dir, got, want)
		}
	}
}

func bundleFixture(t *testing.T, root, category string) {
	t.Helper()
	b := filepath.Join(root, category, "eduardo")
	mustWriteFile(t, filepath.Join(b, "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "git.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "cli", "zsh.nix"), "{ }")
}

func TestWalk_BundleSubmodulesQualified(t *testing.T) {
	root := t.TempDir()
	bundleFixture(t, root, "apps")
	mustWriteFile(t, filepath.Join(root, "apps", "eduardo", "plumbing.nix"), "{ }")

	us, _, err := Walk(root, "", "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for name, path := range map[string]string{
		"eduardo":     "apps/eduardo",
		"eduardo/git": "apps/eduardo/modules/git.nix",
		"eduardo/zsh": "apps/eduardo/modules/cli/zsh.nix",
	} {
		if u := unitByName(t, us, name); u.Path != path {
			t.Errorf("%s Path = %q, want %q", name, u.Path, path)
		}
	}
	if len(us) != 3 {
		t.Errorf("got %d units, want 3: %+v", len(us), us)
	}
}

func TestWalk_NestedBundle(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "apps", "eduardo")
	mustWriteFile(t, filepath.Join(b, "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "nvim", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "nvim", "modules", "lsp.nix"), "{ }")

	us, _, err := Walk(root, "", "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if u := unitByName(t, us, "eduardo/nvim/lsp"); u.Path != "apps/eduardo/modules/nvim/modules/lsp.nix" {
		t.Errorf("Path = %q", u.Path)
	}
	unitByName(t, us, "eduardo/nvim")
}

func TestWalk_SameSubmoduleNameInTwoBundles(t *testing.T) {
	root := t.TempDir()
	for _, b := range []string{"eduardo", "luar"} {
		mustWriteFile(t, filepath.Join(root, "apps", b, "default.nix"), "{ }")
		mustWriteFile(t, filepath.Join(root, "apps", b, "modules", "git.nix"), "{ }")
	}
	us, _, err := Walk(root, "", "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	unitByName(t, us, "eduardo/git")
	unitByName(t, us, "luar/git")
}

func TestWalk_DuplicateInsideOneBundleErrors(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "apps", "eduardo")
	mustWriteFile(t, filepath.Join(b, "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "a", "git.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "b", "git.nix"), "{ }")
	_, _, err := Walk(root, "", "")
	if err == nil || !strings.Contains(err.Error(), "duplicate module name 'eduardo/git'") {
		t.Fatalf("err = %v", err)
	}
}

func TestWalk_BundleModulesWithDefaultNixErrors(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "apps", "eduardo")
	mustWriteFile(t, filepath.Join(b, "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "default.nix"), "{ }")
	_, _, err := Walk(root, "", "")
	if err == nil || !strings.Contains(err.Error(), "apps/eduardo/modules has its own default.nix: a bundle's modules/ folder is not a module") {
		t.Fatalf("err = %v", err)
	}
}

func TestWalk_ModulesFolderUnderCategoryErrors(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "users", "modules", "git.nix"), "{ }")
	_, _, err := Walk(root, "", "")
	if err == nil || !strings.Contains(err.Error(), "users/modules: a folder named modules is only allowed directly inside a module (a bundle)") {
		t.Fatalf("err = %v", err)
	}
}

func TestWalk_LocalBundleShadowsSharedBundle(t *testing.T) {
	root := t.TempDir()
	local := t.TempDir()
	bundleFixture(t, root, "apps")
	b := filepath.Join(local, "eduardo")
	mustWriteFile(t, filepath.Join(b, "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "git.nix"), "{ }")
	mustWriteFile(t, filepath.Join(b, "modules", "extra.nix"), "{ }")

	us, _, err := Walk(root, local, "")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if u := unitByName(t, us, "eduardo"); u.Path != "host/eduardo" || u.Shadows != "apps/eduardo" {
		t.Errorf("bundle = %+v", u)
	}
	if u := unitByName(t, us, "eduardo/git"); u.Path != "host/eduardo/modules/git.nix" || u.Shadows != "apps/eduardo/modules/git.nix" {
		t.Errorf("git = %+v", u)
	}
	if u := unitByName(t, us, "eduardo/extra"); u.Shadows != "apps/eduardo/modules/extra.nix" {
		t.Errorf("extra = %+v", u)
	}
	// The shared-only submodule is dropped with its bundle.
	for _, u := range us {
		if u.Name == "eduardo/zsh" {
			t.Errorf("shared submodule survived: %+v", u)
		}
	}
}

const emptyModule = "{ ... }:\n{\n}\n"

// selectionOf renders a modules.nix selecting items.
func selectionOf(items ...string) string {
	s := "{ ... }:\n{\n  imports = [\n"
	for _, it := range items {
		s += "    " + it + "\n"
	}
	return s + "  ];\n}\n"
}

// A plain tree, no symlinks: modules/host and modules/machine do not exist.
func TestLoadOtherHostUnits(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hosts := filepath.Join(root, ".local", "hosts")
	mustWriteFile(t, filepath.Join(modulesDir, "apps", "git.nix"), emptyModule)
	mustWriteFile(t, config.SelectionPath(filepath.Join(hosts, "a")), selectionOf("../apps/git.nix"))
	mustWriteFile(t, config.SelectionPath(filepath.Join(hosts, "b")), selectionOf("./vpn.nix"))
	mustWriteFile(t, filepath.Join(hosts, "b", "modules", "vpn.nix"), emptyModule)

	h, err := Load(modulesDir, filepath.Join(hosts, "b"), "")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := h.Find("vpn")
	if !ok || m.Path != "host/vpn.nix" || m.Abs != filepath.Join(hosts, "b", "modules", "vpn.nix") {
		t.Fatalf("vpn = %+v, %v", m, ok)
	}
	if _, ok := h.Selected("vpn"); !ok {
		t.Error("vpn not selected on b")
	}

	ha, err := Load(modulesDir, filepath.Join(hosts, "a"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ha.Find("vpn"); ok {
		t.Error("host a sees b's unit")
	}
}

func TestLoadShadow(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostDir := filepath.Join(root, ".local", "hosts", "a")
	mustWriteFile(t, filepath.Join(modulesDir, "apps", "git.nix"), emptyModule)
	mustWriteFile(t, config.SelectionPath(hostDir), selectionOf())
	mustWriteFile(t, filepath.Join(hostDir, "modules", "git.nix"), emptyModule)

	h, err := Load(modulesDir, hostDir, "")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := h.Find("git")
	if m.Path != "host/git.nix" || m.Shadows != "apps/git.nix" {
		t.Fatalf("git = %+v", m)
	}
	if len(h.Modules) != 1 {
		t.Errorf("hidden shared unit still listed: %+v", h.Modules)
	}
	if len(h.Hidden) != 1 || h.Hidden[0].Path != "apps/git.nix" {
		t.Errorf("Hidden = %+v, want the shared git", h.Hidden)
	}
}

func TestScopeOf(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hosts := filepath.Join(root, ".local", "hosts")
	machines := filepath.Join(root, ".local", "machines")
	mustWriteFile(t, filepath.Join(modulesDir, "apps", "git.nix"), emptyModule)
	mustWriteFile(t, filepath.Join(modulesDir, "apps", "vim.nix"), emptyModule)
	for _, h := range []string{"a", "b", "c"} {
		mustWriteFile(t, config.SelectionPath(filepath.Join(hosts, h)), selectionOf())
	}
	for _, m := range []string{"k1", "k2"} {
		mustWriteFile(t, config.SelectionPath(filepath.Join(machines, m)), selectionOf())
	}
	mustWriteFile(t, filepath.Join(hosts, "b", "modules", "git.nix"), emptyModule)
	mustWriteFile(t, filepath.Join(machines, "k2", "modules", "git.nix"), emptyModule)
	mustWriteFile(t, filepath.Join(hosts, "stray", "modules", "x.nix"), emptyModule)

	h := &Host{Name: "a", HostDir: filepath.Join(hosts, "a"), MachineDir: filepath.Join(machines, "k1")}
	got, err := scopeOf(hosts, machines, h, Module{Name: "git", Path: "apps/git.nix"})
	if err != nil {
		t.Fatal(err)
	}
	sel := func(scope, root, name string) SelectionFile {
		return SelectionFile{Scope: scope, File: config.SelectionPath(filepath.Join(root, name))}
	}
	want := reach{
		Selections: []SelectionFile{
			sel(config.ScopeHost, hosts, "a"), sel(config.ScopeHost, hosts, "c"), sel(config.ScopeMachine, machines, "k1"),
		},
		UnitDirs: []string{
			filepath.Join(hosts, "a", "modules"), filepath.Join(hosts, "c", "modules"), filepath.Join(machines, "k1", "modules"),
		},
		SettingsDirs: []string{filepath.Join(hosts, "a"), filepath.Join(hosts, "c"), filepath.Join(machines, "k1")},
		Skipped:      []string{filepath.Join(hosts, "b"), filepath.Join(machines, "k2")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("shared scope = %+v, want %+v", got, want)
	}

	got, err = scopeOf(hosts, machines, h, Module{Name: "priv", Path: "host/priv.nix", Scope: config.ScopeHost})
	if err != nil {
		t.Fatal(err)
	}
	want = reach{
		Selections:   []SelectionFile{sel(config.ScopeHost, hosts, "a")},
		UnitDirs:     []string{filepath.Join(hosts, "a", "modules")},
		SettingsDirs: []string{filepath.Join(hosts, "a"), filepath.Join(machines, "k1")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("host scope = %+v, want %+v", got, want)
	}

	got, err = scopeOf(hosts, machines, h, Module{Name: "boot", Path: "machine/boot.nix", Scope: config.ScopeMachine})
	if err != nil {
		t.Fatal(err)
	}
	want = reach{
		Selections:   []SelectionFile{sel(config.ScopeMachine, machines, "k1")},
		UnitDirs:     []string{filepath.Join(machines, "k1", "modules")},
		SettingsDirs: []string{filepath.Join(machines, "k1")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("machine scope = %+v, want %+v", got, want)
	}
}

// scopes builds modules/, a host folder and a machine folder under one root
// and returns their paths.
func scopes(t *testing.T) (modulesDir, hostDir, machineDir string) {
	t.Helper()
	root := t.TempDir()
	modulesDir = filepath.Join(root, "modules")
	hostDir = filepath.Join(root, ".local", "hosts", "h1")
	machineDir = filepath.Join(root, ".local", "machines", "abcd")
	mustMkdirAll(t, modulesDir)
	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), "{ ... }:\n{\n  imports = [\n  ];\n}\n")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "default.nix"), "{ ... }:\n{\n  imports = [\n  ];\n}\n")
	return
}

func TestWalkScopes(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "packages.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "boot.nix"), "{ }")

	us, hidden, err := Walk(modulesDir, filepath.Join(hostDir, "modules"), filepath.Join(machineDir, "modules"))
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 0 {
		t.Fatalf("hidden = %+v", hidden)
	}
	for name, want := range map[string]Module{
		"luar":     {Path: "users/luar.nix", Scope: ""},
		"packages": {Path: "host/packages.nix", Scope: "host"},
		"boot":     {Path: "machine/boot.nix", Scope: "machine"},
	} {
		u := unitByName(t, us, name)
		if u.Path != want.Path || u.Scope != want.Scope {
			t.Errorf("%s: path %q scope %q, want %q %q", name, u.Path, u.Scope, want.Path, want.Scope)
		}
	}
}

func TestWalkPrecedenceMachineHostShared(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "devices", "laptop.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "laptop.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "laptop.nix"), "{ }")

	us, hidden, err := Walk(modulesDir, filepath.Join(hostDir, "modules"), filepath.Join(machineDir, "modules"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitByName(t, us, "laptop")
	if u.Scope != "machine" || u.Shadows != "devices/laptop.nix" {
		t.Fatalf("winner %+v, want the machine unit shadowing devices/laptop.nix", u)
	}
	if u.StagedPath() != "devices/laptop.nix" {
		t.Errorf("StagedPath = %q", u.StagedPath())
	}
	var hiddenPaths []string
	for _, h := range hidden {
		hiddenPaths = append(hiddenPaths, h.Path)
	}
	sort.Strings(hiddenPaths)
	if want := []string{"devices/laptop.nix", "host/laptop.nix"}; !reflect.DeepEqual(hiddenPaths, want) {
		t.Errorf("hidden %v, want %v", hiddenPaths, want)
	}
}

func TestWalkSkipsReservedRootEntries(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	if err := os.Symlink(filepath.Join(hostDir, "modules"), filepath.Join(modulesDir, "host")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(machineDir, "modules"), filepath.Join(modulesDir, "machine")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(modulesDir, "default.nix"), "{ imports = [ ./host ./machine ]; }")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "packages.nix"), "{ }")
	us, _, err := Walk(modulesDir, filepath.Join(hostDir, "modules"), filepath.Join(machineDir, "modules"))
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || us[0].Path != "host/packages.nix" {
		t.Fatalf("got %+v, want only host/packages.nix", us)
	}
}

func TestLoadReadsBothSelections(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "boot.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), "{ ... }:\n{\n  imports = [\n    ../users/luar.nix\n  ];\n}\n")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "default.nix"), "{ ... }:\n{\n  imports = [\n    ./boot.nix\n    ../users/luar.nix\n  ];\n}\n")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"users/luar.nix", "machine/boot.nix"}; !reflect.DeepEqual(h.Selection(), want) {
		t.Fatalf("Selection() = %v, want %v", h.Selection(), want)
	}
}

func TestRole(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostModules := filepath.Join(root, "host")
	machineModules := filepath.Join(root, "machine")
	for _, f := range []string{
		"users/ana/default.nix",
		"users/solo.nix",
		"users/team/zed/default.nix",
		"desktop/ana2/default.nix",
		"system/core.nix",
		"apps/x/default.nix",
		"apps/x/modules/boot.nix",
	} {
		mustWriteFile(t, filepath.Join(modulesDir, f), "{ }")
	}
	mustWriteFile(t, filepath.Join(hostModules, "bob", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(machineModules, "boot.nix"), "{ }")

	units, _, err := Walk(modulesDir, hostModules, machineModules)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Role{
		"ana":    RoleUser,
		"solo":   RolePlain,
		"zed":    RolePlain,
		"ana2":   RolePlain,
		"bob":    RolePlain,
		"core":   RoleFramework,
		"boot":   RoleBoot,
		"x/boot": RolePlain,
	}
	for name, role := range want {
		m, ok := Find(units, name)
		if !ok {
			t.Fatalf("%s not found", name)
		}
		if got := m.Role(); got != role {
			t.Errorf("%s.Role() = %v, want %v (Path %q, Scope %q)", name, got, role, m.Path, m.Scope)
		}
	}
}

func TestRoleHostBootIsPlain(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostModules := filepath.Join(root, "host")
	mustMkdirAll(t, modulesDir)
	mustWriteFile(t, filepath.Join(hostModules, "boot.nix"), "{ }")

	units, _, err := Walk(modulesDir, hostModules, "")
	if err != nil {
		t.Fatal(err)
	}
	m, _ := Find(units, "boot")
	if m.Role() != RolePlain {
		t.Errorf("host boot Role() = %v, want RolePlain", m.Role())
	}
}

// userFixture writes a user unit users/<name> with submodules subs and a
// selection holding lines.
func userFixture(t *testing.T, modulesDir, name string, subs []string, lines ...string) {
	t.Helper()
	dir := filepath.Join(modulesDir, "users", name)
	mustWriteFile(t, filepath.Join(dir, "default.nix"), "{ }")
	for _, s := range subs {
		mustWriteFile(t, filepath.Join(dir, "modules", s), "{ }")
	}
	mustWriteFile(t, filepath.Join(dir, "modules", "default.nix"), selectionOf(lines...))
}

func TestLoad_UserSelections(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	userFixture(t, modulesDir, "luar", []string{"a.nix"}, "./a.nix")
	userFixture(t, modulesDir, "bob", []string{"b.nix"}, "./b.nix")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), selectionOf("../users/luar"))

	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Users) != 2 || h.Users[0].Scope != "users/bob/modules" || h.Users[1].Scope != "users/luar/modules" {
		t.Fatalf("Users = %+v", h.Users)
	}
	if got := h.Selection(); !equalStrings(got, []string{"users/luar", "users/luar/modules/a.nix"}) {
		t.Errorf("Selection = %v", got)
	}
	if sel := h.SelectedUsers(); len(sel) != 1 || sel[0].Scope != "users/luar/modules" {
		t.Errorf("SelectedUsers = %+v", sel)
	}
	if _, ok := h.Find("luar/default"); ok {
		t.Error("the user's selection was walked as a unit")
	}
}

func TestLoad_UserWithoutSelection(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "luar", "modules", "a.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), selectionOf("../users/luar"))

	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Users) != 0 || h.UserSelection("luar") != nil {
		t.Errorf("Users = %+v", h.Users)
	}
	if got := h.Selection(); !equalStrings(got, []string{"users/luar"}) {
		t.Errorf("Selection = %v", got)
	}
}

func TestWalk_SelectionOnlyAllowedInUsers(t *testing.T) {
	modulesDir, hostDir, _ := scopes(t)
	mustWriteFile(t, filepath.Join(hostDir, "modules", "users", "luar", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "users", "luar", "modules", "default.nix"), selectionOf())
	_, _, err := Walk(modulesDir, filepath.Join(hostDir, "modules"), "")
	if err == nil {
		t.Fatal("a host bundle with modules/default.nix was accepted")
	}
	if want := filepath.Join("users", "luar", "modules") + " has its own default.nix"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, want %q", err, want)
	}
}

func TestUserOf(t *testing.T) {
	modulesDir, _, _ := scopes(t)
	userFixture(t, modulesDir, "luar", []string{"a.nix", "ui/b.nix"})
	mustWriteFile(t, filepath.Join(modulesDir, "users", "bob.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "desktop", "kit", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "desktop", "kit", "modules", "c.nix"), "{ }")
	us, _, err := Walk(modulesDir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"luar": "luar", "luar/a": "luar", "luar/b": "luar", "bob": "", "kit": "", "kit/c": ""} {
		if got := UserOf(unitByName(t, us, name)); got != want {
			t.Errorf("UserOf(%s) = %q, want %q", name, got, want)
		}
	}
	if unitByName(t, us, "luar").Role() != RoleUser || unitByName(t, us, "luar/a").Role() == RoleUser || unitByName(t, us, "bob").Role() == RoleUser {
		t.Error("Role: want RoleUser only for the user unit")
	}
}

func TestLoad_UnselectedUserBrokenSelection(t *testing.T) {
	modulesDir, hostDir, machineDir := scopes(t)
	userFixture(t, modulesDir, "luar", []string{"a.nix"}, "./a.nix")
	userFixture(t, modulesDir, "bob", []string{"b.nix"})
	bob := filepath.Join(modulesDir, "users", "bob", "modules", "default.nix")
	mustWriteFile(t, bob, "{ ... }: { }\n")
	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), selectionOf("../users/luar"))

	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatalf("Load with unselected bob broken: %v", err)
	}
	if h.UserSelection("bob") != nil || h.UserSelection("luar") == nil {
		t.Errorf("Users = %+v", h.Users)
	}

	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), selectionOf("../users/luar", "../users/bob"))
	if _, err := Load(modulesDir, hostDir, machineDir); err == nil || !strings.Contains(err.Error(), bob) {
		t.Errorf("Load with selected bob broken: err = %v", err)
	}

	mustWriteFile(t, filepath.Join(hostDir, "modules", "default.nix"), selectionOf("../users/luar"))
	mustWriteFile(t, filepath.Join(machineDir, "modules", "default.nix"), selectionOf("../users/bob"))
	if _, err := Load(modulesDir, hostDir, machineDir); err == nil || !strings.Contains(err.Error(), bob) {
		t.Errorf("Load with bob selected by the machine only: err = %v", err)
	}
}

func TestCanShadow(t *testing.T) {
	shared := Module{Scope: config.ScopeShared}
	host := Module{Scope: config.ScopeHost}
	machine := Module{Scope: config.ScopeMachine}
	cases := []struct {
		path     string
		existing Module
		want     bool
	}{
		{"apps/x", shared, false},
		{"apps/x", host, false},
		{"apps/x", machine, false},
		{"host/x", shared, true},
		{"host/x", host, false},
		{"host/x", machine, false},
		{"machine/x", shared, true},
		{"machine/x", host, true},
		{"machine/x", machine, false},
	}
	for _, c := range cases {
		if got := CanShadow(c.path, c.existing); got != c.want {
			t.Errorf("CanShadow(%q, %s) = %v, want %v", c.path, c.existing.Scope, got, c.want)
		}
	}
}

func TestBootSelected(t *testing.T) {
	cases := []struct {
		name             string
		sharedUnit       bool // apps/boot.nix exists
		hostUnit         bool // host/boot.nix exists
		machAbsent       bool // machine/boot.nix is missing
		hostSel, machSel []string
		want             bool
	}{
		{"machine selects it", false, false, false, nil, []string{"./boot.nix"}, true},
		{"nothing selects it", false, false, false, nil, nil, false},
		{"pre-heal cross-scope host line", false, false, false, []string{"../machine/boot.nix"}, nil, true},
		{"host line names a host boot the machine shadows", false, true, false, []string{"./boot.nix"}, nil, true},
		{"machine selects it, file missing", false, false, true, nil, []string{"./boot.nix"}, true},
		{"pre-heal cross-scope host line, file missing", false, false, true, []string{"../machine/boot.nix"}, nil, true},
		{"machine selects it, file missing, host boot exists", false, true, true, nil, []string{"./boot.nix"}, true},
		{"machine selects it, file missing, shared boot exists", true, false, true, nil, []string{"./boot.nix"}, true},
		{"host boot selected by the host only, file missing", false, true, true, []string{"./boot.nix"}, nil, false},
		{"shared boot selected by the host only, file missing", true, false, true, []string{"../apps/boot.nix"}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			modulesDir := filepath.Join(root, "modules")
			hostDir := filepath.Join(root, "hosts", "h")
			machineDir := filepath.Join(root, "machines", "k")
			mustMkdirAll(t, modulesDir)
			if c.machAbsent {
				mustMkdirAll(t, filepath.Join(machineDir, "modules"))
			} else {
				mustWriteFile(t, filepath.Join(machineDir, "modules", "boot.nix"), "{ }")
			}
			if c.sharedUnit {
				mustWriteFile(t, filepath.Join(modulesDir, "apps", "boot.nix"), "{ }")
			}
			if c.hostUnit {
				mustWriteFile(t, filepath.Join(hostDir, "modules", "boot.nix"), "{ }")
			}
			mustWriteFile(t, config.SelectionPath(hostDir), selectionOf(c.hostSel...))
			mustWriteFile(t, config.SelectionPath(machineDir), selectionOf(c.machSel...))

			h, err := Load(modulesDir, hostDir, machineDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := h.BootSelected(); got != c.want {
				t.Errorf("BootSelected() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUserPath(t *testing.T) {
	if got := UserPath("ana"); got != "users/ana" {
		t.Errorf("UserPath(ana) = %q", got)
	}
	if got := UserPath(""); got != "users" {
		t.Errorf("UserPath(\"\") = %q", got)
	}
}

func TestFindUser(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "ana", "default.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "services", "eduardo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "solo.nix"), "{ }")
	mustWriteFile(t, filepath.Join(modulesDir, "users", "team", "zed", "default.nix"), "{ }")
	units, _, err := Walk(modulesDir, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if m, ok, err := FindUser(units, "ana"); err != nil || !ok || m.Path != "users/ana" {
		t.Errorf("FindUser(ana) = %v, %v, %v", m.Path, ok, err)
	}
	if _, ok, err := FindUser(units, "nobody"); err != nil || ok {
		t.Errorf("FindUser(nobody) = %v, %v", ok, err)
	}
	_, _, err = FindUser(units, "eduardo")
	if err == nil || !strings.Contains(err.Error(), "modules/services/eduardo.nix") {
		t.Errorf("FindUser(eduardo) err = %v, want one naming modules/services/eduardo.nix", err)
	}
	for name, path := range map[string]string{"solo": "users/solo.nix", "zed": "users/team/zed"} {
		m, ok, err := FindUser(units, name)
		if m.Role() != RolePlain {
			t.Errorf("%s role = %v, want RolePlain", path, m.Role())
		}
		if ok || err == nil || !strings.Contains(err.Error(), "modules/"+path) {
			t.Errorf("FindUser(%s) = %v, %v, want an error naming modules/%s", name, ok, err, path)
		}
	}
}
