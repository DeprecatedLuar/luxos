package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{ }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateHardware_Clean(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, HardwareConfigFile, BootFile, HardwareFile)
	if err := ValidateHardware(dir); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHardware_MissingAndForeign(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, HardwareConfigFile, "stray.nix")
	err := ValidateHardware(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"missing boot.nix", "missing hardware.nix", "stray.nix does not belong here"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestValidateHost_HardwareLink(t *testing.T) {
	newHost := func() string {
		dir := t.TempDir()
		writeFiles(t, dir, plsDontTouchFile, MachineFile, selectionFile)
		return dir
	}
	ok := newHost()
	if err := os.Symlink("../../hardware/x", filepath.Join(ok, "hardware")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHost(ok); err != nil {
		t.Fatalf("symlink rejected: %v", err)
	}
	bad := newHost()
	if err := os.Mkdir(filepath.Join(bad, "hardware"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHost(bad); err == nil || !strings.Contains(err.Error(), "hardware does not belong here") {
		t.Fatalf("directory accepted: %v", err)
	}
}
