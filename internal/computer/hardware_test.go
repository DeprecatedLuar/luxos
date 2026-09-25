package computer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureHardwareConfig_ExistingFileUntouched(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hardware-configuration.nix")
	const original = "{ }: { # hand-written\n}\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	created, err := EnsureHardwareConfig(file)
	if err != nil {
		t.Fatalf("EnsureHardwareConfig: %v", err)
	}
	if created {
		t.Errorf("created = true, want false")
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("content changed: got %q, want %q", got, original)
	}
}
