package paths

import (
	"os/user"
	"path/filepath"
	"testing"
)

func TestResolve_ConfigDirOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-config")
	t.Setenv(configDirEnv, override)

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if p.Config != override {
		t.Fatalf("Config = %q, want %q", p.Config, override)
	}
	if want := filepath.Join(override, localRel); p.Local != want {
		t.Fatalf("Local = %q, want %q", p.Local, want)
	}
	if want := filepath.Join(override, modulesRel); p.Modules != want {
		t.Fatalf("Modules = %q, want %q", p.Modules, want)
	}
}

func TestResolve_ConfigDirOverrideWinsOverHomeAndXDG(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-config")
	t.Setenv(configDirEnv, override)
	t.Setenv(homeEnv, t.TempDir())
	t.Setenv(xdgConfigHomeEnv, t.TempDir())

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Config != override {
		t.Fatalf("Config = %q, want %q", p.Config, override)
	}
}

func TestResolve_SudoUserIgnoresHome(t *testing.T) {
	cur, err := user.Current()
	if err != nil {
		t.Skipf("user.Current unavailable: %v", err)
	}

	t.Setenv(sudoUserEnv, cur.Username)
	t.Setenv(homeEnv, "/nonexistent/fake-home")

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Home != cur.HomeDir {
		t.Fatalf("Home = %q, want %q (sudo user's passwd HomeDir, ignoring $HOME)", p.Home, cur.HomeDir)
	}
	if p.Home == "/nonexistent/fake-home" {
		t.Fatalf("Home incorrectly took $HOME under $SUDO_USER")
	}
}

func TestResolve_HomeEnvOverridesCurrentUser(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv(homeEnv, fakeHome)
	t.Setenv(xdgConfigHomeEnv, "")

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Home != fakeHome {
		t.Fatalf("Home = %q, want %q", p.Home, fakeHome)
	}
	if want := filepath.Join(fakeHome, configRelToHome); p.Config != want {
		t.Fatalf("Config = %q, want %q", p.Config, want)
	}
}

func TestResolve_NoHomeEnvFallsBackToCurrentUser(t *testing.T) {
	cur, err := user.Current()
	if err != nil {
		t.Skipf("user.Current unavailable: %v", err)
	}

	// Explicitly unset HOME via Setenv+empty is not the same as unset, so
	// rely on t.Setenv("") only where empty means "unset" for our own logic.
	t.Setenv(homeEnv, "")

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Home != cur.HomeDir {
		t.Fatalf("Home = %q, want %q (user.Current() fallback)", p.Home, cur.HomeDir)
	}
}

func TestResolve_XDGConfigHomeSet(t *testing.T) {
	fakeHome := t.TempDir()
	xdg := t.TempDir()
	t.Setenv(homeEnv, fakeHome)
	t.Setenv(xdgConfigHomeEnv, xdg)

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(xdg, configDirName); p.Config != want {
		t.Fatalf("Config = %q, want %q", p.Config, want)
	}
}

func TestResolve_XDGConfigHomeUnset(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv(homeEnv, fakeHome)
	t.Setenv(xdgConfigHomeEnv, "")

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(fakeHome, configRelToHome); p.Config != want {
		t.Fatalf("Config = %q, want %q", p.Config, want)
	}
}

func TestResolve_BackupDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(sudoUserEnv, "")
	t.Setenv(backupDirEnv, "")
	t.Setenv(homeEnv, home)

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := filepath.Join(home, backupDirName); p.Backup != want {
		t.Fatalf("Backup = %q, want %q", p.Backup, want)
	}
	if p.SudoUser {
		t.Fatal("SudoUser = true, want false")
	}
}

func TestResolve_BackupEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "backup")
	t.Setenv(backupDirEnv, override)

	p, err := Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Backup != override {
		t.Fatalf("Backup = %q, want %q", p.Backup, override)
	}
}
