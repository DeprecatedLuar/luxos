package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	config_ "github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/paths"
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

//──[per-host scoping of remove/rename]───────────────────────────────────────

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

	if err := config_.EnsureLocalModules(local, modules, "host1"); err != nil {
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

	host1Names, err := modules.ReadSelection(filepath.Join(p.Machines, "host1", "modules.nix"))
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
	us := []modules.Module{
		{Name: "ambxst", Path: "local/ambxst.nix", Shadows: "desktop/shells/ambxst"},
		{Name: "other", Path: "local/other.nix"},
	}
	rows := moduleBuildRows(us, nil, nil, nil, nil, "desktop")
	if len(rows) != 1 || !rows[0].shadow || strings.Join(rows[0].category, "/") != "shells" {
		t.Fatalf("rows = %+v", rows)
	}

	all := moduleBuildRows(us, nil, nil, nil, nil, "")
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
	skipIfNoNix(t)
	dir := t.TempDir()
	decl := func(names ...string) string {
		var b strings.Builder
		b.WriteString("{ ... }: {\n")
		for _, name := range names {
			fmt.Fprintf(&b, "  flake-file.inputs.%s.url = \"github:o/%s\";\n", name, name)
		}
		b.WriteString("}\n")
		return b.String()
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
	write("file.nix", decl("zed", "abc"))
	write("folder/default.nix", "{ ... }: { }\n")
	write("folder/sub/inner.nix", decl("deep"))
	write("none.nix", "{ ... }: { }\n")

	us := []modules.Module{
		{Name: "file", Path: "file.nix", Abs: filepath.Join(dir, "file.nix")},
		{Name: "folder", Path: "folder", Abs: filepath.Join(dir, "folder")},
		{Name: "none", Path: "none.nix", Abs: filepath.Join(dir, "none.nix")},
	}
	rows := []moduleRow{{name: "file", unit: "file"}, {name: "folder", unit: "folder"}, {name: "none", unit: "none"}}
	if err := moduleFillInputs(rows, us); err != nil {
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

func TestModuleBuildRowsModified(t *testing.T) {
	us := []modules.Module{{Name: "a", Path: "a.nix"}, {Name: "b", Path: "b.nix"}, {Name: "c", Path: "c.nix"}}
	enabled := map[string]bool{"a": true, "b": true}
	running := map[string]bool{"a": true, "c": true}
	changed := map[string]bool{"a": true, "b": true, "c": true}
	rows := moduleBuildRows(us, enabled, running, nil, changed, "")
	got := map[string]moduleRow{}
	for _, r := range rows {
		got[r.name] = r
	}
	if r := got["a"]; !r.modified || r.marker != markerEnabledOnly || r.rank != 0 {
		t.Errorf("a = %+v, want modified staged marker rank 0", r)
	}
	if got["b"].modified || got["c"].modified {
		t.Errorf("only enabled+running rows may be modified: b=%+v c=%+v", got["b"], got["c"])
	}
}

func TestModuleRenderModified(t *testing.T) {
	rows := []moduleRow{{name: "a", marker: markerEnabledOnly, modified: true}}
	var tty strings.Builder
	moduleRenderTTY(&tty, rows, "modules/", colorTreePalette)
	if !strings.Contains(tty.String(), colorBlue+markerEnabledOnly) {
		t.Errorf("tty output lacks blue marker: %q", tty.String())
	}
	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	if want := "a\tmodified\t\n"; plain.String() != want {
		t.Errorf("plain = %q, want %q", plain.String(), want)
	}
}

func TestModuleRemovedRows(t *testing.T) {
	us := []modules.Module{{Name: "alive", Path: "alive.nix"}}
	run := []string{
		"./local/debug.nix", "./desktop/shells/ambxst", "./gone.nix", "./alive.nix",
		"./x/dup.nix", "./y/dup.nix", "./desktop/apps/foo/default.nix",
	}
	rows := moduleRemovedRows(run, us, "")
	got := map[string]moduleRow{}
	for _, r := range rows {
		got[r.name] = r
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %+v, want 5 (alive skipped, dup once)", rows)
	}
	for name, cat := range map[string]string{"debug": "local", "ambxst": "desktop/shells", "gone": "", "foo": "desktop/apps"} {
		r, ok := got[name]
		if !ok || !r.removed || r.marker != markerRunningOnly || r.rank != moduleMarkerRank(markerRunningOnly) {
			t.Errorf("%s = %+v", name, r)
		}
		if strings.Join(r.category, "/") != cat {
			t.Errorf("%s category = %v, want %q", name, r.category, cat)
		}
	}
	if _, ok := got["alive"]; ok {
		t.Error("existing unit got a removed row")
	}

	filtered := moduleRemovedRows(run, us, "desktop")
	if len(filtered) != 2 {
		t.Fatalf("filtered = %+v, want ambxst and foo", filtered)
	}
	for _, r := range filtered {
		if r.name == "ambxst" && strings.Join(r.category, "/") != "shells" {
			t.Errorf("ambxst category = %v, want [shells]", r.category)
		}
	}
	if rows := moduleRemovedRows(run, us, "local"); len(rows) != 1 || len(rows[0].category) != 0 {
		t.Errorf("local filter = %+v", rows)
	}
}

func TestModuleRenderRemoved(t *testing.T) {
	rows := moduleRemovedRows([]string{"./local/debug.nix"}, nil, "")
	var tty strings.Builder
	moduleRenderTTY(&tty, rows, "modules/", colorTreePalette)
	if !strings.Contains(tty.String(), colorStrike+"debug") || !strings.Contains(tty.String(), colorRed+markerRunningOnly) {
		t.Errorf("tty output lacks strike/red: %q", tty.String())
	}
	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	if want := "local/debug\tremoved\t\n"; plain.String() != want {
		t.Errorf("plain = %q, want %q", plain.String(), want)
	}
}

const (
	bundleName     = "luar"
	bundlePath     = "users/luar"
	subName        = "luar/demo"
	subPath        = "users/luar/modules/demo.nix"
	emptyImports   = "{ ... }:\n{\n  imports = [\n  ];\n}\n"
	hostEntrypoint = "modules.nix"
)

// bundleFixture is twoHostScopeFixture plus a shared bundle with one
// submodule, the active host's mirror link, and empty selections.
func bundleFixture(t *testing.T) paths.Paths {
	t.Helper()
	p := twoHostScopeFixture(t)
	write(t, filepath.Join(p.Modules, bundlePath, "default.nix"), "{ }\n")
	write(t, filepath.Join(p.Modules, subPath), "{ }\n")
	for _, h := range []string{"host1", "host2"} {
		write(t, filepath.Join(p.Machines, h, hostEntrypoint), emptyImports)
	}
	if err := config_.EnsureMirror(p.Machines, p.Modules, "host1"); err != nil {
		t.Fatalf("EnsureMirror: %v", err)
	}
	return p
}

func hostImports(t *testing.T, p paths.Paths, host string) []string {
	t.Helper()
	got, err := modules.ReadSelection(filepath.Join(p.Machines, host, hostEntrypoint))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestToggleModule_EnableSubmoduleAddsBundleFirst(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)

	if err := toggleModule(p, subName, true); err != nil {
		t.Fatalf("toggleModule: %v", err)
	}
	got := hostImports(t, p, "host1")
	if want := []string{bundlePath, subPath}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("imports = %v, want %v", got, want)
	}
}

func TestToggleModule_DisableBundleRemovesSubmodules(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	if err := toggleModule(p, subName, true); err != nil {
		t.Fatal(err)
	}

	if err := toggleModule(p, bundleName, false); err != nil {
		t.Fatalf("toggleModule: %v", err)
	}
	if got := hostImports(t, p, "host1"); len(got) != 0 {
		t.Errorf("imports = %v, want none", got)
	}
}

func TestToggleModule_DisableSubmoduleKeepsBundle(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	if err := toggleModule(p, subName, true); err != nil {
		t.Fatal(err)
	}
	if err := toggleModule(p, subName, false); err != nil {
		t.Fatal(err)
	}
	if got := hostImports(t, p, "host1"); strings.Join(got, ",") != bundlePath {
		t.Errorf("imports = %v, want only the bundle", got)
	}
}

func TestModuleRemove_BundleRemovesSubmoduleLinesEverywhere(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	for _, h := range []string{"host1", "host2"} {
		write(t, filepath.Join(p.Machines, h, hostEntrypoint),
			"{ ... }:\n{\n  imports = [\n    ./"+bundlePath+"\n    ./"+subPath+"\n  ];\n}\n")
	}

	if err := moduleRemove(p, []string{bundleName, "-y"}); err != nil {
		t.Fatalf("moduleRemove: %v", err)
	}
	for _, h := range []string{"host1", "host2"} {
		if got := hostImports(t, p, h); len(got) != 0 {
			t.Errorf("%s imports = %v, want none", h, got)
		}
	}
	if _, err := os.Stat(filepath.Join(p.Modules, bundlePath)); !os.IsNotExist(err) {
		t.Errorf("bundle still on disk: %v", err)
	}
}

func TestModuleRemove_BundleShadowedHostKeepsSubmoduleLines(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	write(t, filepath.Join(p.Machines, "host2", "modules", bundleName, "default.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host2", "modules", bundleName, "modules", "demo.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host2", hostEntrypoint),
		"{ ... }:\n{\n  imports = [\n    ./local/"+bundleName+"\n    ./local/"+bundleName+"/modules/demo.nix\n  ];\n}\n")
	write(t, filepath.Join(p.Machines, "host1", hostEntrypoint),
		"{ ... }:\n{\n  imports = [\n    ./"+bundlePath+"\n    ./"+subPath+"\n  ];\n}\n")

	if err := moduleRemove(p, []string{bundleName, "-y"}); err != nil {
		t.Fatalf("moduleRemove: %v", err)
	}
	if got := hostImports(t, p, "host2"); len(got) != 2 {
		t.Errorf("host2 imports = %v, want both lines kept", got)
	}
}

func TestModuleRename_SubmoduleStaysInBundle(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	if err := toggleModule(p, subName, true); err != nil {
		t.Fatal(err)
	}

	err := moduleRename(p, []string{subName, "luar/other", "-y"})
	if err == nil || !strings.Contains(err.Error(), "rename keeps a submodule in its bundle: give only the new name") {
		t.Fatalf("err = %v", err)
	}

	if err := moduleRename(p, []string{subName, "demo2", "-y"}); err != nil {
		t.Fatalf("moduleRename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "users/luar/modules/demo2.nix")); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
	if got := hostImports(t, p, "host1"); strings.Join(got, ",") != bundlePath+",users/luar/modules/demo2.nix" {
		t.Errorf("imports = %v", got)
	}
}

func TestModuleRename_SubmoduleCollisionIsQualified(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	write(t, filepath.Join(p.Modules, "users/luar/modules/taken.nix"), "{ }\n")
	// the same bare name in another bundle is not a collision
	write(t, filepath.Join(p.Modules, "users/bob/default.nix"), "{ }\n")
	write(t, filepath.Join(p.Modules, "users/bob/modules/demo2.nix"), "{ }\n")

	err := moduleRename(p, []string{subName, "taken", "-y"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if err := moduleRename(p, []string{subName, "demo2", "-y"}); err != nil {
		t.Fatalf("moduleRename: %v", err)
	}
}

func TestModuleRename_BundleRewritesSubmoduleLines(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	if err := toggleModule(p, subName, true); err != nil {
		t.Fatal(err)
	}

	if err := moduleRename(p, []string{bundleName, "luar2", "-y"}); err != nil {
		t.Fatalf("moduleRename: %v", err)
	}
	want := "users/luar2,users/luar2/modules/demo.nix"
	if got := hostImports(t, p, "host1"); strings.Join(got, ",") != want {
		t.Errorf("imports = %v, want %s", got, want)
	}
}

func bundleUnits() []modules.Module {
	return []modules.Module{
		{Name: "eduardo", Path: "users/eduardo"},
		{Name: "eduardo/git", Path: "users/eduardo/modules/git.nix"},
		{Name: "eduardo/zsh", Path: "users/eduardo/modules/cli/zsh.nix"},
		{Name: "hyprland", Path: "desktop/hyprland.nix"},
	}
}

func TestModuleBuildRows_BundleAndSubmoduleRows(t *testing.T) {
	enabled := map[string]bool{"eduardo": true, "eduardo/git": true}
	rows := moduleBuildRows(bundleUnits(), enabled, nil, nil, nil, "")
	byUnit := map[string]moduleRow{}
	for _, r := range rows {
		byUnit[r.unit] = r
	}
	b := byUnit["eduardo"]
	if !b.bundle || b.subEnabled != 1 || b.subTotal != 2 {
		t.Errorf("bundle row = %+v", b)
	}
	g := byUnit["eduardo/git"]
	if g.name != "git" || g.bundle || strings.Join(g.category, "/") != "users/eduardo/modules" {
		t.Errorf("submodule row = %+v", g)
	}
	if byUnit["hyprland"].bundle {
		t.Error("plain unit marked as bundle")
	}
}

func TestModuleBuildRows_BundleTargetIsRelativeToItsModules(t *testing.T) {
	rows := moduleBuildRows(bundleUnits(), nil, nil, nil, nil, "users/eduardo/modules")
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, r := range rows {
		want := map[string]string{"git": "", "zsh": "cli"}[r.name]
		if strings.Join(r.category, "/") != want {
			t.Errorf("%s category = %v, want %q", r.name, r.category, want)
		}
	}
}

func TestCollapseBundles(t *testing.T) {
	enabled := map[string]bool{"eduardo": true, "eduardo/git": true}
	rows := moduleBuildRows(bundleUnits(), enabled, nil, nil, nil, "")

	top := collapseBundles(rows, "")
	var b strings.Builder
	moduleRenderTTY(&b, top, "modules/", treePalette{})
	out := b.String()
	if !strings.Contains(out, "eduardo+ 1/2") || strings.Contains(out, "git") {
		t.Errorf("collapsed tree:\n%s", out)
	}

	inside := collapseBundles(rows, "eduardo")
	if len(inside) != 2 {
		t.Errorf("bundle view rows = %+v", inside)
	}
}

func TestCollapseBundles_NestedBundleStaysCollapsed(t *testing.T) {
	us := []modules.Module{
		{Name: "eduardo", Path: "users/eduardo"},
		{Name: "eduardo/nvim", Path: "users/eduardo/modules/nvim"},
		{Name: "eduardo/nvim/lsp", Path: "users/eduardo/modules/nvim/modules/lsp.nix"},
	}
	rows := moduleBuildRows(us, nil, nil, nil, nil, "users/eduardo/modules")
	view := collapseBundles(rows, "eduardo")
	if len(view) != 1 || view[0].name != "nvim" || !view[0].bundle || view[0].subTotal != 1 {
		t.Errorf("view = %+v", view)
	}
}

func TestModuleRenderPlain_SubmodulesAreOwnRows(t *testing.T) {
	rows := moduleBuildRows(bundleUnits(), nil, nil, nil, nil, "")
	var b strings.Builder
	moduleRenderPlain(&b, rows)
	if !strings.Contains(b.String(), "users/eduardo/modules/git\toff\t\n") {
		t.Errorf("plain output:\n%s", b.String())
	}
	var j strings.Builder
	if err := moduleRenderJSON(&j, rows); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.String(), "users/eduardo/modules/cli/zsh") {
		t.Errorf("json output:\n%s", j.String())
	}
}

func TestModuleRemovedRows_SubmoduleNameIsBase(t *testing.T) {
	rows := moduleRemovedRows([]string{"./users/eduardo/modules/old.nix"}, nil, "")
	if len(rows) != 1 || rows[0].name != "old" || rows[0].unit != "eduardo/old" {
		t.Errorf("rows = %+v", rows)
	}
}
