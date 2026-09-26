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
	write(t, filepath.Join(p.Machines, "host1", "modules", "hardware", "default.nix"), "{ }\n")
	write(t, filepath.Join(p.Machines, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/hardware\n  ];\n}\n")

	for name, err := range map[string]error{
		"remove": moduleRemove(p, []string{"hardware", "-y"}),
		"rename": moduleRename(p, []string{"hardware", "hw", "-y"}),
	} {
		if err == nil || !strings.Contains(err.Error(), "managed by luxos") || !strings.Contains(err.Error(), "luxos module disable hardware") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(p.Machines, "host1", "modules", "hardware", "default.nix")); err != nil {
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
