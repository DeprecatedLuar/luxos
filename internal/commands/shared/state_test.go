package shared

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/paths"
)

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestNoConfigError(t *testing.T) {
	got := NoConfigError("/home/you/.config/luxos").Error()
	want := "There's no luxos config at /home/you/.config/luxos yet.\nRun `luxos setup` to create one, or pass --config <dir>."
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestNoMachineError(t *testing.T) {
	cfg := t.TempDir()
	machines := filepath.Join(cfg, ".local/machines")

	got := NoMachineError(cfg, machines, "nixos").Error()
	want := "I couldn't find a \"nixos\" machine in " + cfg + ".\nRun `luxos setup`, or pass --machine <name>."
	if got != want {
		t.Errorf("no machines:\ngot  %q\nwant %q", got, want)
	}

	mustWrite(t, filepath.Join(machines, "nuremberg/modules.nix"), "{ imports = [ ]; }\n")
	mustWrite(t, filepath.Join(machines, "cathode/modules.nix"), "{ imports = [ ]; }\n")
	got = NoMachineError(cfg, machines, "nixos").Error()
	want = "I couldn't find a \"nixos\" machine in " + cfg + "\n(has cathode, nuremberg).\nRun `luxos setup`, or pass --machine <name>."
	if got != want {
		t.Errorf("with machines:\ngot  %q\nwant %q", got, want)
	}
}

func TestResolveHost_States(t *testing.T) {
	t.Setenv(paths.ConfigDirEnv, "") // ResolveHost sets it; restored after the test
	empty := t.TempDir()
	if _, _, _, err := ResolveHost(map[string]string{"config": empty, "machine": ""}); err == nil || err.Error() != NoConfigError(empty).Error() {
		t.Errorf("empty config: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "absent")
	if _, _, _, err := ResolveHost(map[string]string{"config": missing}); err == nil || err.Error() != NoConfigError(missing).Error() {
		t.Errorf("missing config folder: %v", err)
	}

	// An explicit --machine is always the no-machine error, even with no config.
	if _, _, _, err := ResolveHost(map[string]string{"config": empty, "machine": "ghost"}); err == nil ||
		err.Error() != NoMachineError(empty, filepath.Join(empty, ".local/machines"), "ghost").Error() {
		t.Errorf("explicit machine: %v", err)
	}

	cfg := t.TempDir()
	mustWrite(t, filepath.Join(cfg, ".local/machines/cathode/modules.nix"), "{ imports = [ ]; }\n")
	_, host, dir, err := ResolveHost(map[string]string{"config": cfg, "machine": "cathode"})
	if err != nil || host != "cathode" || dir != filepath.Join(cfg, ".local/machines/cathode") {
		t.Errorf("ready: %q %q %v", host, dir, err)
	}
}

func TestActiveHost_States(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	empty := t.TempDir()
	t.Setenv(paths.ConfigDirEnv, empty)
	p, err := paths.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ActiveHost(p); err == nil || err.Error() != NoConfigError(empty).Error() {
		t.Errorf("empty config: %v", err)
	}

	cfg := t.TempDir()
	mustWrite(t, filepath.Join(cfg, ".local/machines/other-box/modules.nix"), "{ imports = [ ]; }\n")
	t.Setenv(paths.ConfigDirEnv, cfg)
	if p, err = paths.Resolve(); err != nil {
		t.Fatal(err)
	}
	if _, err := ActiveHost(p); err == nil || err.Error() != NoMachineError(cfg, p.Machines, hostname).Error() {
		t.Errorf("no machine: %v", err)
	}

	mustWrite(t, filepath.Join(cfg, ".local/machines/box/modules/.keep"), "")
	if err := os.MkdirAll(filepath.Join(cfg, "modules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.local/machines/box/modules", filepath.Join(cfg, "modules/local")); err != nil {
		t.Fatal(err)
	}
	if host, err := ActiveHost(p); err != nil || host != "box" {
		t.Errorf("linked: %q, %v; want box", host, err)
	}
}
