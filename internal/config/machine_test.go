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

func TestLoadMachine_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.toml")
	writeFile(t, path, `
timeZone = "America/Sao_Paulo"
locale = "en_US.UTF-8"
stateVersion = "24.05"
`)

	m, warnings, err := LoadMachine(path)
	if err != nil {
		t.Fatalf("LoadMachine: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if m.TimeZone != "America/Sao_Paulo" || m.Locale != "en_US.UTF-8" || m.StateVersion != "24.05" {
		t.Fatalf("unexpected Machine: %+v", m)
	}
}

func TestLoadMachine_MissingRequiredKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.toml")
	writeFile(t, path, `locale = "en_US.UTF-8"`)

	_, _, err := LoadMachine(path)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"Missing required key: timeZone",
		"Missing required key: stateVersion",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "Missing required key: locale") {
		t.Errorf("error should not report locale as missing: %v", err)
	}
}

func TestLoadMachine_HostNameRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.toml")
	writeFile(t, path, `
hostName = "paraloid"
timeZone = "America/Sao_Paulo"
locale = "en_US.UTF-8"
stateVersion = "24.05"
`)

	_, _, err := LoadMachine(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "'hostName' is no longer a machine.toml key: remove hostName; the directory name sets it"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err.Error(), want)
	}
}

func TestLoadMachine_UsersRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.toml")
	writeFile(t, path, `
users = ["luar"]
timeZone = "America/Sao_Paulo"
locale = "en_US.UTF-8"
stateVersion = "24.05"
`)

	_, _, err := LoadMachine(path)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "'users' is no longer a machine.toml key: remove users; select users in modules/default.nix"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err.Error(), want)
	}
}

func TestLoadMachine_UnknownKeyWarns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "machine.toml")
	writeFile(t, path, `
timeZone = "America/Sao_Paulo"
locale = "en_US.UTF-8"
stateVersion = "24.05"
extra = "value"
`)

	m, warnings, err := LoadMachine(path)
	if err != nil {
		t.Fatalf("LoadMachine: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	want := "unknown key 'extra' in " + path
	if warnings[0] != want {
		t.Errorf("warning = %q, want %q", warnings[0], want)
	}
	if m.StateVersion != "24.05" {
		t.Fatalf("unexpected Machine: %+v", m)
	}
}

func TestResolveMachine_Found(t *testing.T) {
	localDir := t.TempDir()
	machineDir := filepath.Join(localDir, "paraloid")
	writeFile(t, filepath.Join(machineDir, "machine.toml"), `timeZone = "UTC"`)

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
	if !strings.Contains(err.Error(), "no machine.toml under "+wantDir) {
		t.Errorf("error %q missing dir mention", err.Error())
	}
	if !strings.Contains(err.Error(), "Pass --machine <name> if this machine was renamed or isn't named after $(hostname).") {
		t.Errorf("error %q missing hint", err.Error())
	}
}
