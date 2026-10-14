package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/hardware"
)

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, n)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{ }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateMachine(t *testing.T) {
	dir := t.TempDir()
	if err := ValidateMachine(dir); err == nil ||
		!strings.Contains(err.Error(), "missing "+HardwareConfigFile) ||
		!strings.Contains(err.Error(), "missing modules/default.nix") ||
		!strings.Contains(err.Error(), "missing modules/boot.nix") {
		t.Fatalf("empty folder: got %v", err)
	}
	writeFiles(t, dir, HardwareConfigFile, "modules/default.nix", "modules/boot.nix", "notes.md")
	if err := ValidateMachine(dir); err != nil {
		t.Fatalf("complete folder with an extra entry: %v", err)
	}
}

func TestSeedSelectionOnlyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	created, err := ensureMachineSelection(dir)
	if err != nil || !created {
		t.Fatalf("first: created=%v err=%v", created, err)
	}
	data, _ := os.ReadFile(SelectionPath(dir))
	if !strings.Contains(string(data), "./boot.nix") {
		t.Fatalf("seed does not select ./boot.nix:\n%s", data)
	}
	if err := os.WriteFile(SelectionPath(dir), []byte("{ ... }:\n{\n  imports = [ ];\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	created, err = ensureMachineSelection(dir)
	if err != nil || created {
		t.Fatalf("second: created=%v err=%v", created, err)
	}
	data, _ = os.ReadFile(SelectionPath(dir))
	if strings.Contains(string(data), "boot") {
		t.Fatal("an existing selection was rewritten")
	}
}

func TestValidateHost_HardwareEntryRejected(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, plsDontTouchFile, HostFile, filepath.Join(ScopeModulesDir, DefaultFile))
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

func TestLinkedMachineDir(t *testing.T) {
	root := t.TempDir()
	machine := filepath.Join(root, ".local", "machines", "abcd")
	mustMkdirAll(t, filepath.Join(machine, "modules"))
	modulesDir := filepath.Join(root, "modules")
	mustMkdirAll(t, modulesDir)
	if _, err := LinkedMachineDir(modulesDir); !errors.Is(err, ErrNoMachineLink) {
		t.Fatalf("missing link: got %v, want ErrNoMachineLink", err)
	}
	if err := os.Symlink("../.local/machines/abcd/modules", filepath.Join(modulesDir, MachineLinkName)); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(machine)
	if err != nil {
		t.Fatal(err)
	}
	got, err := LinkedMachineDir(modulesDir)
	if err != nil || got != want {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
}
