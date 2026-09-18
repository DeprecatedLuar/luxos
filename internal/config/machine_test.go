package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeRequiredFiles(t *testing.T, hostDir string) {
	t.Helper()
	writeFile(t, filepath.Join(hostDir, ".plsdonttouch.nix"), "{ system.stateVersion = \"24.05\"; }\n")
	writeFile(t, filepath.Join(hostDir, "machine.nix"), "{ time.timeZone = \"UTC\"; }\n")
	writeFile(t, filepath.Join(hostDir, "modules.nix"), "{ imports = []; }\n")
}

func TestResolveMachine_Found(t *testing.T) {
	localDir := t.TempDir()
	machineDir := filepath.Join(localDir, "paraloid")
	writeFile(t, filepath.Join(machineDir, "modules.nix"), "{ imports = []; }\n")

	dir, err := ResolveMachine(localDir, "paraloid")
	if err != nil {
		t.Fatalf("ResolveMachine: %v", err)
	}
	if dir != machineDir {
		t.Fatalf("dir = %q, want %q", dir, machineDir)
	}
}

func TestResolveMachine_NotFound(t *testing.T) {
	localDir := t.TempDir()

	_, err := ResolveMachine(localDir, "nuremberg")
	if err == nil {
		t.Fatal("expected error")
	}
	wantDir := filepath.Join(localDir, "nuremberg")
	if !strings.Contains(err.Error(), "no modules.nix under "+wantDir) {
		t.Errorf("error %q missing dir mention", err.Error())
	}
	if !strings.Contains(err.Error(), "Pass --machine <name> if this machine was renamed or isn't named after $(hostname).") {
		t.Errorf("error %q missing hint", err.Error())
	}
}

func TestValidateMachine_Valid(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)

	if err := ValidateMachine(dir); err != nil {
		t.Fatalf("ValidateMachine: %v", err)
	}
}

func TestValidateMachine_ValidWithLockAndModules(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)
	writeFile(t, filepath.Join(dir, "flake.lock"), "{}\n")
	writeFile(t, filepath.Join(dir, "modules", "foo.nix"), "{ }\n")

	if err := ValidateMachine(dir); err != nil {
		t.Fatalf("ValidateMachine: %v", err)
	}
}

func TestValidateMachine_ValidWithEmptyModulesDir(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "modules"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ValidateMachine(dir); err != nil {
		t.Fatalf("ValidateMachine: %v", err)
	}
}

func TestValidateMachine_MissingEach(t *testing.T) {
	for _, missing := range []string{".plsdonttouch.nix", "machine.nix", "modules.nix"} {
		t.Run(missing, func(t *testing.T) {
			dir := t.TempDir()
			writeRequiredFiles(t, dir)
			if err := os.Remove(filepath.Join(dir, missing)); err != nil {
				t.Fatal(err)
			}

			err := ValidateMachine(dir)
			if err == nil {
				t.Fatal("expected error")
			}
			want := "missing " + missing
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q missing %q", err.Error(), want)
			}
		})
	}
}

func TestValidateMachine_StrayFile(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)
	writeFile(t, filepath.Join(dir, "hardware.nix"), "{ }\n")

	err := ValidateMachine(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "hardware.nix does not belong here"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err.Error(), want)
	}
}

func TestValidateMachine_ModulesAsFile(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)
	writeFile(t, filepath.Join(dir, "modules"), "not a dir\n")

	err := ValidateMachine(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "modules does not belong here"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err.Error(), want)
	}
}

func TestValidateMachine_LockAsDirectory(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "flake.lock"), 0755); err != nil {
		t.Fatal(err)
	}

	err := ValidateMachine(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "flake.lock does not belong here"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err.Error(), want)
	}
}

func TestValidateMachine_MultipleProblemsInOneError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "machine.nix"), "{ }\n")
	writeFile(t, filepath.Join(dir, "modules.nix"), "{ imports = []; }\n")
	writeFile(t, filepath.Join(dir, "hardware.nix"), "{ }\n")
	// .plsdonttouch.nix missing entirely.

	err := ValidateMachine(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"missing .plsdonttouch.nix",
		"hardware.nix does not belong here",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestProtectMachine_ChangesModeOnce(t *testing.T) {
	dir := t.TempDir()
	writeRequiredFiles(t, dir)
	path := filepath.Join(dir, ".plsdonttouch.nix")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := ProtectMachine(dir)
	if err != nil {
		t.Fatalf("ProtectMachine: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0444 {
		t.Fatalf("mode = %o, want 0444", info.Mode().Perm())
	}

	changed2, err := ProtectMachine(dir)
	if err != nil {
		t.Fatalf("ProtectMachine (2nd): %v", err)
	}
	if changed2 {
		t.Fatalf("2nd changed = true, want false")
	}
}
