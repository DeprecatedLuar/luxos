package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

func testMachine() Machine {
	return Machine{
		Name:         "cathode",
		StateVersion: "25.11",
		Values:       MachineValues{Channel: "nixos-25.11", Timezone: "Europe/Lisbon", Locale: "pt_PT.UTF-8", Keyboard: "pt"},
		Selection:    []string{"local/hardware-support", "local/packages.nix", "users/ana"},
	}
}

func TestStarterModules_Parse(t *testing.T) {
	skipIfNoNix(t)
	src, err := templates.Dir(starterModulesDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	err = fs.WalkDir(src, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, rel)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, strings.ReplaceAll(rel, "/", "_"))
		if err := os.WriteFile(path, data, 0644); err != nil {
			return err
		}
		assertParses(t, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCopyStarterModules(t *testing.T) {
	modulesDir := filepath.Join(t.TempDir(), "config/modules")
	if err := CopyStarterModules(modulesDir); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"desktop/compositors/hyprland.nix", "desktop/greeters/greetd.nix", "hardware/laptop/options.nix", "unstable.nix"} {
		if _, err := os.Stat(filepath.Join(modulesDir, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	if err := CopyStarterModules(modulesDir); err == nil {
		t.Error("copying over existing files must fail")
	}
}

func TestCreateMachine(t *testing.T) {
	machines := filepath.Join(t.TempDir(), ".local/machines")
	if err := CreateMachine(machines, testMachine()); err != nil {
		t.Fatal(err)
	}
	hostDir := filepath.Join(machines, "cathode")

	if err := ValidateHost(hostDir); err != nil {
		t.Errorf("ValidateHost: %v", err)
	}
	info, err := os.Stat(filepath.Join(hostDir, ".plsdonttouch.nix"))
	if err != nil || info.Mode().Perm() != 0444 {
		t.Errorf(".plsdonttouch.nix mode: %v %v", info, err)
	}
	if got := readFile(t, filepath.Join(hostDir, ".plsdonttouch.nix")); !strings.Contains(got, `system.stateVersion = "25.11";`) {
		t.Errorf(".plsdonttouch.nix = %q", got)
	}
	if got := readFile(t, filepath.Join(hostDir, "machine.nix")); !strings.Contains(got, `time.timeZone = "Europe/Lisbon";`) {
		t.Errorf("machine.nix = %q", got)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "modules/packages.nix")); err != nil {
		t.Errorf("modules/packages.nix: %v", err)
	}
	if got := readFile(t, filepath.Join(hostDir, "modules.nix")); !strings.Contains(got,
		"  imports = [\n    ./local/hardware-support\n    ./local/packages.nix\n    ./users/ana\n  ];") {
		t.Errorf("modules.nix = %q", got)
	}

	if err := CreateMachine(machines, testMachine()); err == nil {
		t.Error("creating an existing machine must fail")
	}
}

func TestCreateMachine_UnparsableValueWritesNothing(t *testing.T) {
	skipIfNoNix(t)
	machines := filepath.Join(t.TempDir(), ".local/machines")
	m := testMachine()
	m.Values.Timezone = `Europe/"Lisbon`
	err := CreateMachine(machines, m)
	if err == nil || !strings.Contains(err.Error(), "machine.nix") {
		t.Fatalf("err = %v, want a parse error naming machine.nix", err)
	}
	if _, err := os.Stat(filepath.Join(machines, "cathode")); !os.IsNotExist(err) {
		t.Errorf("machine folder exists after a refused write: %v", err)
	}
}
