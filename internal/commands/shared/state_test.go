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

func TestNoHostError(t *testing.T) {
	cfg := t.TempDir()
	hosts := filepath.Join(cfg, ".local/hosts")

	got := NoHostError(cfg, hosts, "nixos").Error()
	want := "I couldn't find a \"nixos\" host in " + cfg + ".\nRun `luxos setup`, or pass --host <name>."
	if got != want {
		t.Errorf("no hosts:\ngot  %q\nwant %q", got, want)
	}

	mustWrite(t, filepath.Join(hosts, "nuremberg/modules/default.nix"), "{ imports = [ ]; }\n")
	mustWrite(t, filepath.Join(hosts, "cathode/modules/default.nix"), "{ imports = [ ]; }\n")
	got = NoHostError(cfg, hosts, "nixos").Error()
	want = "I couldn't find a \"nixos\" host in " + cfg + "\n(has cathode, nuremberg).\nRun `luxos setup`, or pass --host <name>."
	if got != want {
		t.Errorf("with hosts:\ngot  %q\nwant %q", got, want)
	}
}

func TestResolveHost_States(t *testing.T) {
	t.Setenv(paths.ConfigDirEnv, "") // ResolveHost sets it; restored after the test
	empty := t.TempDir()
	if _, _, _, err := ResolveHost(map[string]string{"config": empty, "host": ""}); err == nil || err.Error() != NoConfigError(empty).Error() {
		t.Errorf("empty config: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "absent")
	if _, _, _, err := ResolveHost(map[string]string{"config": missing}); err == nil || err.Error() != NoConfigError(missing).Error() {
		t.Errorf("missing config folder: %v", err)
	}

	// An explicit --host is always the no-host error, even with no config.
	if _, _, _, err := ResolveHost(map[string]string{"config": empty, "host": "ghost"}); err == nil ||
		err.Error() != NoHostError(empty, filepath.Join(empty, ".local/hosts"), "ghost").Error() {
		t.Errorf("explicit host: %v", err)
	}

	cfg := t.TempDir()
	mustWrite(t, filepath.Join(cfg, ".local/hosts/cathode/modules/default.nix"), "{ imports = [ ]; }\n")
	_, host, dir, err := ResolveHost(map[string]string{"config": cfg, "host": "cathode"})
	if err != nil || host != "cathode" || dir != filepath.Join(cfg, ".local/hosts/cathode") {
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
	mustWrite(t, filepath.Join(cfg, ".local/hosts/other-box/modules/default.nix"), "{ imports = [ ]; }\n")
	t.Setenv(paths.ConfigDirEnv, cfg)
	if p, err = paths.Resolve(); err != nil {
		t.Fatal(err)
	}
	if _, err := ActiveHost(p); err == nil || err.Error() != NoHostError(cfg, p.Hosts, hostname).Error() {
		t.Errorf("no host: %v", err)
	}

	mustWrite(t, filepath.Join(cfg, ".local/hosts/box/modules/.keep"), "")
	if err := os.MkdirAll(filepath.Join(cfg, "modules"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.local/hosts/box/modules", filepath.Join(cfg, "modules/host")); err != nil {
		t.Fatal(err)
	}
	if host, err := ActiveHost(p); err != nil || host != "box" {
		t.Errorf("linked: %q, %v; want box", host, err)
	}
}
