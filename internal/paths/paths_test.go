package paths

import (
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
