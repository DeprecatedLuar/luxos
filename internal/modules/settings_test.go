package modules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

const optionsDefault = "{ ... }: { imports = [ ./options.nix ]; }\n"

// settingsHost builds a modules dir with the given units (path -> file
// content) and a host selecting the given lines.
func settingsHost(t *testing.T, units map[string]string, selection ...string) *Host {
	t.Helper()
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostDir := filepath.Join(root, "machines", "host1")
	for path, content := range units {
		mustWriteFile(t, filepath.Join(modulesDir, path), content)
	}
	var b strings.Builder
	b.WriteString("{ ... }:\n{\n  imports = [\n")
	for _, s := range selection {
		b.WriteString("    ./" + s + "\n")
	}
	b.WriteString("  ];\n}\n")
	mustWriteFile(t, filepath.Join(hostDir, "modules.nix"), b.String())
	h, err := Load(modulesDir, hostDir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestNamespace(t *testing.T) {
	if got := Namespace("eduardo/git"); !reflect.DeepEqual(got, []string{"eduardo", "git"}) {
		t.Errorf("Namespace = %v", got)
	}
}

func TestSettings_ValidUnit(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{
		"laptop/default.nix": optionsDefault,
		"laptop/options.nix": `{ lib, ... }: { options.laptop.limit = lib.mkOption { type = lib.types.int; default = 80; }; }`,
		"plain.nix":          "{ }\n",
	}, "laptop")
	us, err := h.Settings(h.Modules)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || us[0].Module.Name != "laptop" || len(us[0].Problems) > 0 {
		t.Fatalf("Settings = %+v", us)
	}
	want := []nix.Setting{{Path: []string{"laptop", "limit"}, Value: float64(80)}}
	if !reflect.DeepEqual(us[0].Declared, want) {
		t.Errorf("Declared = %+v", us[0].Declared)
	}
	if us[0].File != filepath.Join(h.HostDir, "settings", "laptop.nix") {
		t.Errorf("File = %s", us[0].File)
	}
}

func TestSettings_RuleViolations(t *testing.T) {
	skipIfNoNix(t)
	cases := map[string]struct{ defaultNix, options, want string }{
		"config block":    {optionsDefault, `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; config.x = 1; }`, "only declares options"},
		"wrong namespace": {optionsDefault, `{ lib, ... }: { options.other.a = lib.mkOption { type = lib.types.int; default = 1; }; }`, "must be options.laptop.<key>"},
		"nested":          {optionsDefault, `{ lib, ... }: { options.laptop.a.b = lib.mkOption { type = lib.types.int; default = 1; }; }`, "must be options.laptop.<key>"},
		"not an option":   {optionsDefault, `{ lib, ... }: { options.laptop.a = 5; }`, "is not a lib.mkOption"},
		"no type":         {optionsDefault, `{ lib, ... }: { options.laptop.a = lib.mkOption { default = 1; }; }`, "has no type"},
		"no default":      {optionsDefault, `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; }; }`, "has no default"},
		"path default":    {optionsDefault, `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.path; default = ./options.nix; }; }`, "not a plain value"},
		"pkgs header":     {optionsDefault, `{ lib, pkgs, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; }`, "takes only lib"},
		"not imported":    {"{ ... }: { }\n", `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; }`, "does not import ./options.nix"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := settingsHost(t, map[string]string{"laptop/default.nix": c.defaultNix, "laptop/options.nix": c.options}, "laptop")
			us, err := h.Settings(h.Modules)
			if err != nil {
				t.Fatal(err)
			}
			if len(us) != 1 || !strings.Contains(strings.Join(us[0].Problems, "\n"), c.want) {
				t.Errorf("Problems = %v, want one containing %q", us, c.want)
			}
		})
	}
}

func TestSettings_UnparsableSettingsFile(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{
		"laptop/default.nix": optionsDefault,
		"laptop/options.nix": `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; }`,
	}, "laptop")
	mustWriteFile(t, filepath.Join(h.HostDir, "settings", "laptop.nix"), "{ laptop.a = ; }\n")
	us, err := h.Settings(h.Modules)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || !strings.Contains(strings.Join(us[0].Problems, "\n"), "does not parse") {
		t.Errorf("Problems = %+v", us)
	}
}

const laptopOptions = `{ lib, ... }: {
  options.laptop.limit = lib.mkOption { type = lib.types.int; default = 80; };
  options.laptop.mode = lib.mkOption { type = lib.types.str; default = "balanced"; };
}`

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSyncSettings_CreatesForSelectedOnly(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{
		"laptop/default.nix": optionsDefault,
		"laptop/options.nix": laptopOptions,
		"other/default.nix":  optionsDefault,
		"other/options.nix":  `{ lib, ... }: { options.other.a = lib.mkOption { type = lib.types.int; default = 1; }; }`,
	}, "laptop")
	rep, err := SyncSettings(h)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(h.HostDir, "settings", "laptop.nix")
	if !reflect.DeepEqual(rep.Created, []string{file}) {
		t.Errorf("Created = %v", rep.Created)
	}
	if got, want := readString(t, file), "{\n  laptop.limit = 80;\n  laptop.mode = \"balanced\";\n}\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(h.HostDir, "settings", "other.nix")); !os.IsNotExist(err) {
		t.Errorf("unselected unit got a settings file: %v", err)
	}

	again, err := SyncSettings(h)
	if err != nil || len(again.Created)+len(again.Appended)+len(again.Commented) != 0 {
		t.Errorf("second sync = %+v, %v; want nothing to do", again, err)
	}
}

func TestSyncSettings_AppendsAndComments(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{"laptop/default.nix": optionsDefault, "laptop/options.nix": laptopOptions}, "laptop")
	file := filepath.Join(h.HostDir, "settings", "laptop.nix")
	mustWriteFile(t, file, "{\n  laptop.mode = \"performance\";\n  laptop.gone = 1;\n}\n")
	rep, err := SyncSettings(h)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  laptop.mode = \"performance\";\n  # laptop.gone = 1;\n  laptop.limit = 80;\n}\n"
	if got := readString(t, file); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(rep.Appended, []SettingsChange{{File: file, Keys: []string{"laptop.limit"}}}) {
		t.Errorf("Appended = %+v", rep.Appended)
	}
	if !reflect.DeepEqual(rep.Commented, []SettingsChange{{File: file, Keys: []string{"laptop.gone"}}}) {
		t.Errorf("Commented = %+v", rep.Commented)
	}
}

func TestSyncSettings_RedeclaredKeyKeepsComment(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{"laptop/default.nix": optionsDefault, "laptop/options.nix": laptopOptions}, "laptop")
	file := filepath.Join(h.HostDir, "settings", "laptop.nix")
	mustWriteFile(t, file, "{\n  # laptop.limit = 90;\n  laptop.mode = \"a\";\n}\n")
	if _, err := SyncSettings(h); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, file), "{\n  # laptop.limit = 90;\n  laptop.mode = \"a\";\n  laptop.limit = 80;\n}\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestSyncSettings_BrokenUnitSkipped(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{
		"laptop/default.nix": optionsDefault,
		"laptop/options.nix": laptopOptions,
		"bad/default.nix":    optionsDefault,
		"bad/options.nix":    `{ lib, ... }: { options.bad.a = lib.mkOption { type = lib.types.int; }; }`,
	}, "laptop", "bad")
	rep, err := SyncSettings(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Broken) != 1 || rep.Broken[0].Module.Name != "bad" {
		t.Errorf("Broken = %+v", rep.Broken)
	}
	if len(rep.Created) != 1 {
		t.Errorf("Created = %v, want the healthy unit's file", rep.Created)
	}
}

func TestMoveSettings_BundleFolderMoves(t *testing.T) {
	root := t.TempDir()
	machines := filepath.Join(root, "machines")
	for _, host := range []string{"host1", "host2"} {
		mustWriteFile(t, filepath.Join(machines, host, "modules.nix"), "{ imports = [ ]; }\n")
		mustWriteFile(t, filepath.Join(machines, host, "settings", "eduardo.nix"), "{ }\n")
		mustWriteFile(t, filepath.Join(machines, host, "settings", "eduardo", "git.nix"), "{ }\n")
	}
	kept, err := MoveSettings(machines, "eduardo", "luar", "", []string{"host2"})
	if err != nil || len(kept) > 0 {
		t.Fatalf("MoveSettings: %v %v", kept, err)
	}
	for _, p := range []string{"host1/settings/luar.nix", "host1/settings/luar/git.nix", "host2/settings/eduardo.nix", "host2/settings/eduardo/git.nix"} {
		if _, err := os.Stat(filepath.Join(machines, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(machines, "host1/settings/eduardo.nix")); !os.IsNotExist(err) {
		t.Errorf("old file still on host1: %v", err)
	}
}

func TestMoveSettings_RemoveAndKept(t *testing.T) {
	root := t.TempDir()
	machines := filepath.Join(root, "machines")
	mustWriteFile(t, filepath.Join(machines, "host1", "modules.nix"), "{ imports = [ ]; }\n")
	mustWriteFile(t, filepath.Join(machines, "host1", "settings", "a.nix"), "{ }\n")
	mustWriteFile(t, filepath.Join(machines, "host1", "settings", "b.nix"), "{ }\n")

	kept, err := MoveSettings(machines, "a", "b", "host1", nil)
	if err != nil || !reflect.DeepEqual(kept, []string{filepath.Join(machines, "host1", "settings", "a.nix")}) {
		t.Errorf("rename onto an existing file: kept = %v, err = %v", kept, err)
	}
	if _, err := MoveSettings(machines, "a", "", "host1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(machines, "host1", "settings", "a.nix")); !os.IsNotExist(err) {
		t.Errorf("remove left the file: %v", err)
	}
}

func TestStagedSettings(t *testing.T) {
	h := settingsHost(t, map[string]string{
		"laptop/default.nix": optionsDefault,
		"laptop/options.nix": laptopOptions,
		"nofile/default.nix": optionsDefault,
		"nofile/options.nix": laptopOptions,
		"off/default.nix":    optionsDefault,
		"off/options.nix":    laptopOptions,
	}, "laptop", "nofile")
	mustWriteFile(t, filepath.Join(h.HostDir, "settings", "laptop.nix"), "{ }\n")
	mustWriteFile(t, filepath.Join(h.HostDir, "settings", "off.nix"), "{ }\n")
	if got := h.StagedSettings(); !reflect.DeepEqual(got, []string{"laptop"}) {
		t.Errorf("StagedSettings = %v, want [laptop]", got)
	}
}

func TestSyncUnitSettings_Unselected(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{
		"laptop/default.nix": optionsDefault,
		"laptop/options.nix": laptopOptions,
		"other/default.nix":  optionsDefault,
	}, "other")
	m, ok := h.Find("laptop")
	if !ok {
		t.Fatal("laptop not found")
	}
	file, rep, err := SyncUnitSettings(h, m)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(h.HostDir, "settings", "laptop.nix"); file != want {
		t.Errorf("file = %s, want %s", file, want)
	}
	if !reflect.DeepEqual(rep.Created, []string{file}) {
		t.Errorf("Created = %v", rep.Created)
	}
	if got, want := readString(t, file), "{\n  laptop.limit = 80;\n  laptop.mode = \"balanced\";\n}\n"; got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestSyncUnitSettings_NoOptions(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{"plain/default.nix": optionsDefault}, "plain")
	m, ok := h.Find("plain")
	if !ok {
		t.Fatal("plain not found")
	}
	if _, _, err := SyncUnitSettings(h, m); err == nil || !strings.Contains(err.Error(), "has no settings") {
		t.Errorf("err = %v, want 'has no settings'", err)
	}
	if _, err := os.Stat(filepath.Join(h.HostDir, "settings", "plain.nix")); !os.IsNotExist(err) {
		t.Errorf("settings file created: %v", err)
	}
}

const describedOptions = `{ lib, ... }: {
  options.laptop.mode = lib.mkOption { type = lib.types.str; default = "a"; description = "a | b"; };
  options.laptop.limit = lib.mkOption { type = lib.types.int; default = 80; };
}
`

func TestSyncSettings_DescriptionCommentsCreateAppendRefresh(t *testing.T) {
	skipIfNoNix(t)
	h := settingsHost(t, map[string]string{"laptop/default.nix": optionsDefault, "laptop/options.nix": describedOptions}, "laptop")
	file := filepath.Join(h.HostDir, "settings", "laptop.nix")

	if _, err := SyncSettings(h); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, file), "{\n  laptop.limit = 80;\n  laptop.mode = \"a\"; # a | b\n}\n"; got != want {
		t.Errorf("created = %q, want %q", got, want)
	}

	mustWriteFile(t, file, "{\n  laptop.mode = \"b\"; # stale\n}\n")
	if _, err := SyncSettings(h); err != nil {
		t.Fatal(err)
	}
	if got, want := readString(t, file), "{\n  laptop.mode = \"b\"; # a | b\n  laptop.limit = 80;\n}\n"; got != want {
		t.Errorf("healed = %q, want %q", got, want)
	}
}
