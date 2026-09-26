package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/links"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

func TestModuleMarker(t *testing.T) {
	cases := []struct {
		enabled, running bool
		want             string
	}{
		{true, true, markerEnabledBoth},
		{true, false, markerEnabledOnly},
		{false, true, markerRunningOnly},
		{false, false, markerNeither},
	}
	for _, c := range cases {
		if got := moduleMarker(c.enabled, c.running); got != c.want {
			t.Errorf("moduleMarker(%v, %v) = %q, want %q", c.enabled, c.running, got, c.want)
		}
	}
}

func TestModuleMarkerRank(t *testing.T) {
	cases := []struct {
		marker string
		want   int
	}{
		{markerEnabledOnly, 0},
		{markerEnabledBoth, 1},
		{markerRunningOnly, 2},
		{markerPulled, 3},
		{markerNeither, 4},
	}
	for _, c := range cases {
		if got := moduleMarkerRank(c.marker); got != c.want {
			t.Errorf("moduleMarkerRank(%q) = %d, want %d", c.marker, got, c.want)
		}
	}
}

func TestModuleSortRows(t *testing.T) {
	rows := []moduleRow{
		{name: "zeta", marker: markerNeither, rank: 4},
		{name: "delta", marker: markerPulled, rank: 3},
		{name: "alpha", marker: markerEnabledOnly, rank: 0},
		{name: "beta", marker: markerEnabledOnly, rank: 0},
		{name: "gamma", marker: markerEnabledBoth, rank: 1},
	}
	moduleSortRows(rows)

	want := []string{"alpha", "beta", "gamma", "delta", "zeta"}
	for i, name := range want {
		if rows[i].name != name {
			t.Fatalf("rows[%d].name = %q, want %q (order: %+v)", i, rows[i].name, name, rows)
		}
	}
}

func TestModuleSortRows_RankBeforeName(t *testing.T) {
	// A lower-ranked "zzz" must sort before a higher-ranked "aaa".
	rows := []moduleRow{
		{name: "aaa", rank: 3},
		{name: "zzz", rank: 0},
	}
	moduleSortRows(rows)
	if rows[0].name != "zzz" || rows[1].name != "aaa" {
		t.Fatalf("got order %+v, want zzz before aaa", rows)
	}
}

func TestUserTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo", "users/foo"},
		{"users/foo", "users/foo"},
	}
	for _, c := range cases {
		if got := userTarget(c.in); got != c.want {
			t.Errorf("userTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUserCategory(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "users"},
		{"desktop", "users/desktop"},
	}
	for _, c := range cases {
		if got := userCategory(c.in); got != c.want {
			t.Errorf("userCategory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsFrameworkPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"system", true},
		{"system/desktop", true},
		{"users/luar", false},
		{"misc/foo", false},
	}
	for _, c := range cases {
		if got := isFrameworkPath(c.path); got != c.want {
			t.Errorf("isFrameworkPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestJoinCategory(t *testing.T) {
	cases := []struct {
		category, base, want string
	}{
		{"", "foo", "foo"},
		{"misc", "foo", "misc/foo"},
		{"users", "foo.nix", "users/foo.nix"},
	}
	for _, c := range cases {
		if got := joinCategory(c.category, c.base); got != c.want {
			t.Errorf("joinCategory(%q, %q) = %q, want %q", c.category, c.base, got, c.want)
		}
	}
}

//──[Phase 4: per-host scoping of remove/rename]─────────────────────────────

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

func write(t *testing.T, path, content string) {
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

// twoHostScopeFixture builds a config tree with two hosts' local modules
// dirs and modules/local pointed at host1 (the active host).
func twoHostScopeFixture(t *testing.T) paths.Paths {
	t.Helper()
	root := t.TempDir()
	config := filepath.Join(root, "config")
	local := filepath.Join(config, ".local", "machines")
	modules := filepath.Join(config, "modules")
	mustMkdirAll(t, modules)

	if err := links.EnsureLocalModules(local, modules, "host1"); err != nil {
		t.Fatalf("EnsureLocalModules: %v", err)
	}
	// host2 has no active symlink, but still needs its own local dir.
	mustMkdirAll(t, filepath.Join(local, "host2", "modules"))

	return paths.Paths{
		Home:           root,
		User:           "test",
		Config:         config,
		Machines:       local,
		Modules:        modules,
		Staging:        filepath.Join(root, "staging"),
		RunningModules: filepath.Join(root, "run-modules.nix"),
	}
}

func TestModuleRemove_LocalUnitLeavesOtherHostsSameNameUntouched(t *testing.T) {
	skipIfNoNix(t)
	p := twoHostScopeFixture(t)

	write(t, filepath.Join(p.Machines, "host1", "modules", "foo.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host2", "modules", "foo.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")
	write(t, filepath.Join(p.Machines, "host2", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")

	if err := moduleRemove(p, []string{"foo", "-y"}); err != nil {
		t.Fatalf("moduleRemove: %v", err)
	}

	host1Names, err := imports.List(filepath.Join(p.Machines, "host1", "modules.nix"))
	if err != nil {
		t.Fatalf("List(host1): %v", err)
	}
	if len(host1Names) != 0 {
		t.Errorf("host1 modules.nix = %v, want empty (import removed)", host1Names)
	}

	host2Content := mustReadFile(t, filepath.Join(p.Machines, "host2", "modules.nix"))
	if !strings.Contains(host2Content, "./local/foo.nix") {
		t.Errorf("host2 modules.nix was touched, want untouched:\n%s", host2Content)
	}
	if _, err := os.Stat(filepath.Join(p.Machines, "host2", "modules", "foo.nix")); err != nil {
		t.Errorf("host2's local foo.nix should still exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Machines, "host1", "modules", "foo.nix")); !os.IsNotExist(err) {
		t.Errorf("host1's local foo.nix should have been removed, stat err = %v", err)
	}
}

func TestModuleRemoveRename_HardwareUnitRefused(t *testing.T) {
	skipIfNoNix(t)
	p := twoHostScopeFixture(t)
	write(t, filepath.Join(p.Machines, "host1", "modules", "hardware-support", "default.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/hardware-support\n  ];\n}\n")

	for name, err := range map[string]error{
		"remove": moduleRemove(p, []string{"hardware-support", "-y"}),
		"rename": moduleRename(p, []string{"hardware-support", "hw", "-y"}),
	} {
		if err == nil || !strings.Contains(err.Error(), "managed by luxos") || !strings.Contains(err.Error(), "luxos module disable hardware-support") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(p.Machines, "host1", "modules", "hardware-support", "default.nix")); err != nil {
		t.Errorf("hardware folder was touched: %v", err)
	}
}

func TestModuleRename_SharedUnitRewritesNonActiveHostsLocalModule(t *testing.T) {
	skipIfNoNix(t)
	p := twoHostScopeFixture(t)

	write(t, filepath.Join(p.Modules, "wayland.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host2", "modules", "x.nix"),
		`{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`+"\n")
	write(t, filepath.Join(p.Machines, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./wayland.nix\n  ];\n}\n")
	write(t, filepath.Join(p.Machines, "host2", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/x.nix\n  ];\n}\n")

	if err := moduleRename(p, []string{"wayland", "wl", "-y"}); err != nil {
		t.Fatalf("moduleRename: %v", err)
	}

	host2Content := mustReadFile(t, filepath.Join(p.Machines, "host2", "modules", "x.nix"))
	if !strings.Contains(host2Content, `"wl"`) || strings.Contains(host2Content, `"wayland"`) {
		t.Errorf("host2's local x.nix not rewritten: %q", host2Content)
	}

	host1Content := mustReadFile(t, filepath.Join(p.Machines, "host1", "modules.nix"))
	if !strings.Contains(host1Content, "./wl.nix") {
		t.Errorf("host1 modules.nix not rewritten to wl.nix: %q", host1Content)
	}
}

// A bare `module` or `user` prints its help page without resolving paths.
func TestBareModuleAndUserPrintHelp(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("LUXOS_CONFIG_DIR", "")
	if err := Module(nil); err != nil {
		t.Errorf("Module(nil) = %v, want nil", err)
	}
	if err := User(nil); err != nil {
		t.Errorf("User(nil) = %v, want nil", err)
	}
}

func TestModuleRename_SharedUnitShadowedOnHostLeavesThatHostUntouched(t *testing.T) {
	skipIfNoNix(t)
	p := twoHostScopeFixture(t)

	write(t, filepath.Join(p.Modules, "wayland.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host2", "modules", "wayland.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host2", "modules", "x.nix"),
		`{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`+"\n")
	write(t, filepath.Join(p.Machines, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./wayland.nix\n  ];\n}\n")
	host2Sel := "{ ... }:\n{\n  imports = [\n    ./local/wayland.nix\n    ./local/x.nix\n  ];\n}\n"
	write(t, filepath.Join(p.Machines, "host2", "modules.nix"), host2Sel)

	if err := moduleRename(p, []string{"wayland", "wl", "-y"}); err != nil {
		t.Fatalf("moduleRename: %v", err)
	}

	if got := mustReadFile(t, filepath.Join(p.Machines, "host2", "modules.nix")); got != host2Sel {
		t.Errorf("shadowing host's modules.nix changed:\n%s", got)
	}
	if got := mustReadFile(t, filepath.Join(p.Machines, "host2", "modules", "x.nix")); !strings.Contains(got, `"wayland"`) {
		t.Errorf("shadowing host's local reference changed: %q", got)
	}
	if got := mustReadFile(t, filepath.Join(p.Machines, "host1", "modules.nix")); !strings.Contains(got, "./wl.nix") {
		t.Errorf("host1 not rewritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "wl.nix")); err != nil {
		t.Errorf("shared unit not renamed: %v", err)
	}
}

func TestModuleRename_LocalUnitMayShadowSharedName(t *testing.T) {
	skipIfNoNix(t)
	p := twoHostScopeFixture(t)

	write(t, filepath.Join(p.Modules, "wayland.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host1", "modules", "mine.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/mine.nix\n  ];\n}\n")

	if err := moduleRename(p, []string{"mine", "wayland", "-y"}); err != nil {
		t.Fatalf("moduleRename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Machines, "host1", "modules", "wayland.nix")); err != nil {
		t.Errorf("local unit not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "wayland.nix")); err != nil {
		t.Errorf("shared unit must stay: %v", err)
	}
}

func TestModuleBuildRows_ShadowSitsAtOriginalCategoryUnderlined(t *testing.T) {
	us := []units.Unit{
		{Name: "ambxst", Path: "local/ambxst.nix", Shadows: "desktop/shells/ambxst"},
		{Name: "other", Path: "local/other.nix"},
	}
	rows := moduleBuildRows(us, nil, nil, nil, "desktop")
	if len(rows) != 1 || !rows[0].shadow || strings.Join(rows[0].category, "/") != "shells" {
		t.Fatalf("rows = %+v", rows)
	}

	all := moduleBuildRows(us, nil, nil, nil, "")
	var b strings.Builder
	moduleRenderTTY(&b, all, "modules/", colorTreePalette)
	if !strings.Contains(b.String(), colorUnderline+"ambxst"+colorReset) {
		t.Errorf("shadow not underlined:\n%q", b.String())
	}
	if strings.Count(b.String(), "ambxst") != 1 || !strings.Contains(b.String(), "shells/") {
		t.Errorf("shadow not listed once under shells/:\n%q", b.String())
	}
	b.Reset()
	moduleRenderTTY(&b, all, "modules/", treePalette{})
	if strings.Contains(b.String(), "\x1b") {
		t.Errorf("plain palette emitted escapes: %q", b.String())
	}
}

func TestModuleRenderPlainStateAndInputs(t *testing.T) {
	rows := []moduleRow{
		{category: []string{"desktop", "shells"}, name: "ambxst", marker: markerEnabledBoth, inputs: []string{"ambxst", "axctl"}},
		{name: "base", marker: markerEnabledOnly},
		{name: "old", marker: markerRunningOnly},
		{name: "dep", marker: markerPulled},
		{name: "idle", marker: markerNeither},
	}
	var b strings.Builder
	moduleRenderPlain(&b, rows)
	want := "base\tstaged\t\ndep\tpulled\t\ndesktop/shells/ambxst\tactive\tambxst axctl\nidle\toff\t\nold\tleftover\t\n"
	if b.String() != want {
		t.Errorf("got %q want %q", b.String(), want)
	}

	var tty strings.Builder
	moduleRenderTTY(&tty, rows, "modules/", treePalette{})
	if !strings.Contains(tty.String(), "ambxst"+flakeMark+"\n") {
		t.Errorf("flake mark missing:\n%s", tty.String())
	}
}

func TestModuleFillInputs(t *testing.T) {
	dir := t.TempDir()
	decl := func(name string) string {
		return "{ ... }: {\n  flake-file.inputs." + name + ".url = \"github:o/" + name + "\";\n}\n"
	}
	write := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("file.nix", decl("zed")+decl("abc")+decl("zed"))
	write("folder/default.nix", "{ ... }: { }\n")
	write("folder/sub/inner.nix", decl("deep"))
	write("none.nix", "{ ... }: { }\n")

	us := []units.Unit{{Name: "file", Path: "file.nix"}, {Name: "folder", Path: "folder"}, {Name: "none", Path: "none.nix"}}
	rows := []moduleRow{{name: "file"}, {name: "folder"}, {name: "none"}}
	if err := moduleFillInputs(rows, us, dir); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(rows[0].inputs, ","); got != "abc,zed" {
		t.Errorf("file inputs = %q", got)
	}
	if got := strings.Join(rows[1].inputs, ","); got != "deep" {
		t.Errorf("folder inputs = %q", got)
	}
	if len(rows[2].inputs) != 0 {
		t.Errorf("none inputs = %v", rows[2].inputs)
	}
}

func TestModuleRenderJSON(t *testing.T) {
	rows := []moduleRow{
		{category: []string{"desktop", "shells"}, name: "ambxst", marker: markerEnabledBoth, inputs: []string{"ambxst", "axctl"}},
		{name: "base", marker: markerEnabledOnly},
	}
	var b strings.Builder
	if err := moduleRenderJSON(&b, rows); err != nil {
		t.Fatal(err)
	}
	want := `[
  {
    "path": "base",
    "state": "staged",
    "inputs": []
  },
  {
    "path": "desktop/shells/ambxst",
    "state": "active",
    "inputs": [
      "ambxst",
      "axctl"
    ]
  }
]
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestModuleListJSONConflicts(t *testing.T) {
	for _, flag := range []string{"--raw", "--flat"} {
		err := moduleList(paths.Paths{}, []string{"--json", flag})
		if err == nil || !strings.Contains(err.Error(), "--json") || !strings.Contains(err.Error(), flag) {
			t.Errorf("--json %s: got %v", flag, err)
		}
	}
}
