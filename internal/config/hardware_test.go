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
	writeFiles(t, dir, DefaultFile, HardwareConfigFile, BootFile, HardwareFile)
	if err := ValidateHardware(dir); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHardware_ExtraFileAllowed(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, DefaultFile, HardwareConfigFile, BootFile, HardwareFile, "extra.nix")
	if err := ValidateHardware(dir); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHardware_Missing(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, HardwareConfigFile, BootFile)
	err := ValidateHardware(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"missing default.nix", "missing hardware.nix"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestValidateHardware_NotRegular(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, HardwareConfigFile, BootFile, HardwareFile)
	if err := os.Mkdir(filepath.Join(dir, DefaultFile), 0755); err != nil {
		t.Fatal(err)
	}
	err := ValidateHardware(dir)
	if err == nil || !strings.Contains(err.Error(), "default.nix is not a regular file") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateHost_HardwareEntryRejected(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, plsDontTouchFile, MachineFile, selectionFile)
	if err := os.Symlink("../../hardware/x", filepath.Join(dir, "hardware")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHost(dir); err == nil || !strings.Contains(err.Error(), "hardware does not belong here") {
		t.Fatalf("hardware link accepted: %v", err)
	}
}
