package modules

import (
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
