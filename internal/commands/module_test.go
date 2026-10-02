package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	config_ "github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

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

func TestUnderFrameworkCategory(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"system", true},
		{"system/desktop", true},
		{"systemd/foo", false},
		{"users/luar", false},
		{"misc/foo", false},
	}
	for _, c := range cases {
		if got := underCategory(c.path, moduleFrameworkCategory); got != c.want {
			t.Errorf("underCategory(%q, %q) = %v, want %v", c.path, moduleFrameworkCategory, got, c.want)
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
	rows := moduleRows(statusesFor(us, nil, nil), "desktop")
	if len(rows) != 1 || !rows[0].shadow || strings.Join(rows[0].category, "/") != "shells" {
		t.Fatalf("rows = %+v", rows)
	}

	all := moduleRows(statusesFor(us, nil, nil), "")
	var b strings.Builder
	ui.Tree(&b, uiRows(all), "modules/", ui.Palette{})
	if strings.Count(b.String(), "ambxst") != 1 || !strings.Contains(b.String(), "shells/") {
		t.Errorf("shadow not listed once under shells/:\n%q", b.String())
	}
	for _, r := range uiRows(all) {
		if r.Name == "ambxst" && !r.Underline {
			t.Errorf("shadow row not underlined: %+v", r)
		}
		if r.Name == "other" && r.Underline {
			t.Errorf("plain row underlined: %+v", r)
		}
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
	want := "base\tstaged\t\tok\ndep\tpulled\t\tok\ndesktop/shells/ambxst\tactive\tambxst axctl\tok\nidle\toff\t\tok\nold\tleftover\t\tok\n"
	if b.String() != want {
		t.Errorf("got %q want %q", b.String(), want)
	}

	var tty strings.Builder
	ui.Tree(&tty, uiRows(rows), "modules/", ui.Palette{})
	if !strings.Contains(tty.String(), "ambxst "+flakeMark+"\n") {
		t.Errorf("flake mark missing:\n%s", tty.String())
	}
}

func TestModuleRenderJSON(t *testing.T) {
	rows := []moduleRow{
		{category: []string{"desktop", "shells"}, name: "ambxst", marker: markerEnabledBoth, inputs: []string{"ambxst", "axctl"}},
		{name: "base", marker: markerEnabledOnly, broken: true},
	}
	var b strings.Builder
	if err := moduleRenderJSON(&b, rows); err != nil {
		t.Fatal(err)
	}
	want := `[
  {
    "path": "base",
    "state": "staged",
    "inputs": [],
    "ok": false
  },
  {
    "path": "desktop/shells/ambxst",
    "state": "active",
    "inputs": [
      "ambxst",
      "axctl"
    ],
    "ok": true
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

func TestModuleRenderModified(t *testing.T) {
	rows := []moduleRow{{name: "a", marker: markerEnabledOnly, modified: true}}
	if got := uiRows(rows)[0]; got.Color != ui.ColorBlue || got.Marker != markerEnabledOnly {
		t.Errorf("modified row = %+v, want the blue staged marker", got)
	}
	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	if want := "a\tmodified\t\tok\n"; plain.String() != want {
		t.Errorf("plain = %q, want %q", plain.String(), want)
	}
}

func TestModuleRenderRemoved(t *testing.T) {
	rows := moduleRows(removedStatuses("local/debug.nix"), "")
	if got := uiRows(rows)[0]; !got.Strike || got.Color != ui.ColorRed || got.Marker != markerRunningOnly {
		t.Errorf("removed row = %+v, want a struck red leftover marker", got)
	}
	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	if want := "local/debug\tremoved\t\tok\n"; plain.String() != want {
		t.Errorf("plain = %q, want %q", plain.String(), want)
	}
	if got := uiRows(rows)[0]; len(got.Marks) != 0 {
		t.Errorf("removed row marks = %+v, want none", got.Marks)
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
	rows := moduleRows(statusesFor(bundleUnits(), enabled, nil), "")
	byUnit := map[string]moduleRow{}
	for _, r := range rows {
		byUnit[r.unit] = r
	}
	b := byUnit["eduardo"]
	if !b.bundle || b.subEnabled != 1 {
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
	rows := moduleRows(statusesFor(bundleUnits(), nil, nil), "users/eduardo/modules")
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
	rows := moduleRows(statusesFor(bundleUnits(), enabled, nil), "")

	top := collapseBundles(rows, "")
	var b strings.Builder
	ui.Tree(&b, uiRows(top), "modules/", ui.Palette{})
	out := b.String()
	if !strings.Contains(out, markerBundle+" eduardo 1\n") || strings.Contains(out, "git") {
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
	rows := moduleRows(statusesFor(us, nil, nil), "users/eduardo/modules")
	view := collapseBundles(rows, "eduardo")
	if len(view) != 1 || view[0].name != "nvim" || !view[0].bundle {
		t.Errorf("view = %+v", view)
	}
}

func TestModuleRenderPlain_SubmodulesAreOwnRows(t *testing.T) {
	rows := moduleRows(statusesFor(bundleUnits(), nil, nil), "")
	var b strings.Builder
	moduleRenderPlain(&b, rows)
	if !strings.Contains(b.String(), "users/eduardo/modules/git\toff\t\tok\n") {
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
	rows := moduleRows(removedStatuses("users/eduardo/modules/old.nix"), "")
	if len(rows) != 1 || rows[0].name != "old" || rows[0].unit != "eduardo/old" {
		t.Errorf("rows = %+v", rows)
	}
}

// statusesFor derives the statuses moduleRows maps from selection and
// running name sets.
func statusesFor(us []modules.Module, enabled, running map[string]bool) []modules.Status {
	var out []modules.Status
	for _, u := range us {
		state := modules.Off
		switch {
		case enabled[u.Name] && running[u.Name]:
			state = modules.Active
		case enabled[u.Name]:
			state = modules.Staged
		case running[u.Name]:
			state = modules.Leftover
		}
		out = append(out, modules.Status{Module: u, State: state})
	}
	return out
}

// removedStatuses is the status of each running import that names no module.
func removedStatuses(paths ...string) []modules.Status {
	var out []modules.Status
	for _, p := range paths {
		out = append(out, modules.Status{
			Module: modules.Module{Name: modules.NameFromPath(p), Path: strings.TrimSuffix(p, "/default.nix")},
			State:  modules.Removed,
		})
	}
	return out
}

func TestModuleRowsStateMapping(t *testing.T) {
	cases := []struct {
		state    modules.State
		marker   string
		rank     int
		modified bool
		removed  bool
	}{
		{modules.Active, markerEnabledBoth, 1, false, false},
		{modules.Staged, markerEnabledOnly, 0, false, false},
		{modules.Modified, markerEnabledOnly, 0, true, false},
		{modules.Leftover, markerRunningOnly, 2, false, false},
		{modules.Removed, markerRunningOnly, 2, false, true},
		{modules.Pulled, markerPulled, 3, false, false},
		{modules.Off, markerNeither, 4, false, false},
	}
	for _, c := range cases {
		rows := moduleRows([]modules.Status{{Module: modules.Module{Name: "a", Path: "a.nix"}, State: c.state}}, "")
		if len(rows) != 1 {
			t.Fatalf("%s: rows = %+v", c.state, rows)
		}
		r := rows[0]
		if r.marker != c.marker || r.rank != c.rank || r.modified != c.modified || r.removed != c.removed {
			t.Errorf("%s: row = %+v", c.state, r)
		}
	}
}

func TestModuleRowsRemovedCategories(t *testing.T) {
	sts := removedStatuses("local/debug.nix", "desktop/shells/ambxst", "gone.nix", "desktop/apps/foo/default.nix")
	rows := moduleRows(sts, "")
	got := map[string]string{}
	for _, r := range rows {
		got[r.name] = strings.Join(r.category, "/")
	}
	want := map[string]string{"debug": "local", "ambxst": "desktop/shells", "gone": "", "foo": "desktop/apps"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("categories = %v, want %v", got, want)
	}

	filtered := moduleRows(sts, "desktop")
	if len(filtered) != 2 {
		t.Fatalf("filtered = %+v, want ambxst and foo", filtered)
	}
	for _, r := range filtered {
		if r.name == "ambxst" && strings.Join(r.category, "/") != "shells" {
			t.Errorf("ambxst category = %v, want [shells]", r.category)
		}
	}
	if rows := moduleRows(sts, "local"); len(rows) != 1 || len(rows[0].category) != 0 {
		t.Errorf("local filter = %+v", rows)
	}
}

func TestModuleMarksDimOutsideBuild(t *testing.T) {
	rows := []moduleRow{
		{name: "on", marker: markerEnabledBoth, inputs: []string{"x"}, broken: true},
		{name: "dep", marker: markerPulled, broken: true},
		{name: "idle", marker: markerNeither, inputs: []string{"x"}, broken: true},
		{name: "old", marker: markerRunningOnly, broken: true},
	}
	want := map[string][]ui.Mark{
		"on":   {{Glyph: flakeMark, Color: ui.ColorNix}, {Glyph: brokenMark, Color: ui.ColorYellow}},
		"dep":  {{Glyph: brokenMark, Color: ui.ColorYellow}},
		"idle": {{Glyph: flakeMark, Color: ui.ColorLine}, {Glyph: brokenMark, Color: ui.ColorLine}},
		"old":  {{Glyph: brokenMark, Color: ui.ColorLine}},
	}
	for _, r := range uiRows(rows) {
		if !reflect.DeepEqual(r.Marks, want[r.Name]) {
			t.Errorf("%s marks = %+v, want %+v", r.Name, r.Marks, want[r.Name])
		}
	}
}

func TestModuleBundleRowGlyphAndCount(t *testing.T) {
	rows := moduleRows(statusesFor(bundleUnits(), map[string]bool{"eduardo": true}, nil), "")
	for _, r := range uiRows(collapseBundles(rows, "")) {
		if r.Name != "eduardo" {
			continue
		}
		if r.Marker != markerBundle || r.Color != ui.ColorTeal || r.Count != "0" {
			t.Errorf("bundle row = %+v, want a teal %s with count 0", r, markerBundle)
		}
	}
	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	if !strings.Contains(plain.String(), "users/eduardo\tstaged\t\tok\n") {
		t.Errorf("plain keeps the state word:\n%s", plain.String())
	}
}

func TestModuleRowsCarryBroken(t *testing.T) {
	sts := []modules.Status{{Module: modules.Module{Name: "a", Path: "a.nix"}, State: modules.Off, Broken: true}}
	rows := moduleRows(sts, "")
	var plain strings.Builder
	moduleRenderPlain(&plain, rows)
	if plain.String() != "a\toff\t\tbroken\n" {
		t.Errorf("plain = %q", plain.String())
	}
}
