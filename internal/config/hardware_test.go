package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/hardware"
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
	writeFiles(t, dir, plsDontTouchFile, MachineFile, SelectionFile)
	if err := os.Symlink("../../hardware/x", filepath.Join(dir, "hardware")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHost(dir); err == nil || !strings.Contains(err.Error(), "hardware does not belong here") {
		t.Fatalf("hardware link accepted: %v", err)
	}
}

// writeMounts writes a mounts file under dir holding lines and returns its path.
func writeMounts(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "mounts")
	writeFile(t, path, strings.Join(lines, "\n")+"\n")
	return path
}

// efiSys marks sysDir as EFI firmware.
func efiSys(t *testing.T, sysDir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(sysDir, "firmware", "efi", "efivars"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureHardwareConfig_ExistingFileUntouched(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hardware-configuration.nix")
	const original = "{ }: { # hand-written\n}\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	created, err := ensureHardwareConfig(file)
	if err != nil {
		t.Fatalf("ensureHardwareConfig: %v", err)
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

func TestEnsureBoot_ExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	bootFile := filepath.Join(dir, "boot.nix")
	original := []byte("{ ... }: { }\n")
	if err := os.WriteFile(bootFile, original, 0600); err != nil {
		t.Fatalf("write existing boot.nix: %v", err)
	}

	created, err := ensureBoot(bootFile, filepath.Join(dir, "sys"), filepath.Join(dir, "mounts"))
	if err != nil {
		t.Fatalf("ensureBoot: %v", err)
	}
	if created {
		t.Errorf("ensureBoot: created = true, want false")
	}

	got, err := os.ReadFile(bootFile)
	if err != nil {
		t.Fatalf("read boot.nix: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("ensureBoot changed content: got %q, want %q", got, original)
	}
	info, err := os.Stat(bootFile)
	if err != nil {
		t.Fatalf("stat boot.nix: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("ensureBoot changed mode: got %v, want 0600", info.Mode().Perm())
	}
}

func TestEnsureBoot_MissingFileWritesDetected(t *testing.T) {
	dir := t.TempDir()
	sysDir := filepath.Join(dir, "sys")
	efiSys(t, sysDir)
	mountsFile := writeMounts(t, dir, "/dev/sda1 /boot vfat rw 0 0")
	bootFile := filepath.Join(dir, "boot.nix")

	created, err := ensureBoot(bootFile, sysDir, mountsFile)
	if err != nil {
		t.Fatalf("ensureBoot: %v", err)
	}
	if !created {
		t.Errorf("ensureBoot: created = false, want true")
	}

	want, err := hardware.RenderBoot(hardware.Loader{EFI: true, Target: "/boot"})
	if err != nil {
		t.Fatalf("RenderBoot: %v", err)
	}
	got, err := os.ReadFile(bootFile)
	if err != nil {
		t.Fatalf("read boot.nix: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("ensureBoot content mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	info, err := os.Stat(bootFile)
	if err != nil {
		t.Fatalf("stat boot.nix: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("ensureBoot mode = %v, want %v", info.Mode().Perm(), 0644)
	}
}

func TestEnsureBoot_UndetectableErrorsMentionsFile(t *testing.T) {
	dir := t.TempDir()
	sysDir := filepath.Join(dir, "sys")
	// No EFI firmware and no mounts at all: BIOS detection also fails.
	mountsFile := writeMounts(t, dir)
	bootFile := filepath.Join(dir, "boot.nix")

	created, err := ensureBoot(bootFile, sysDir, mountsFile)
	if err == nil {
		t.Fatalf("ensureBoot: want error, got nil")
	}
	if created {
		t.Errorf("ensureBoot: created = true, want false")
	}
	if !strings.Contains(err.Error(), bootFile) {
		t.Errorf("ensureBoot error %q does not mention %q", err.Error(), bootFile)
	}
	if _, statErr := os.Lstat(bootFile); !os.IsNotExist(statErr) {
		t.Errorf("ensureBoot: boot.nix was written despite the error")
	}
}

func TestLinkedHardwareDir(t *testing.T) {
	root := t.TempDir()
	machines := filepath.Join(root, ".local", "machines")
	modulesDir := filepath.Join(root, "modules")
	hwRoot := filepath.Join(root, ".local", "hardware")
	if err := os.MkdirAll(filepath.Join(hwRoot, "k1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(modulesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// No modules/local link at all.
	if _, err := LinkedHardwareDir(modulesDir); err == nil || !strings.Contains(err.Error(), "run 'luxos rebuild build' real quick") {
		t.Fatalf("without modules/local: err = %v, want the missing-link message", err)
	}

	// modules/local exists, hardware-support does not.
	if err := EnsureLocalModules(machines, modulesDir, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := LinkedHardwareDir(modulesDir); err == nil || !strings.Contains(err.Error(), "run 'luxos rebuild build' real quick") {
		t.Fatalf("without hardware-support: err = %v, want the missing-link message", err)
	}

	if err := ensureHardwareLink(machines, hwRoot, "a", "k1"); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(hwRoot, "k1"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := LinkedHardwareDir(modulesDir)
	if err != nil || got != want {
		t.Errorf("LinkedHardwareDir = %q, %v; want %q", got, err, want)
	}
}
