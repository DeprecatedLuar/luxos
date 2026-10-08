package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserAccount_Wheel(t *testing.T) {
	on, err := UserAccount("ana", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(on), "users.users.ana = {") {
		t.Errorf("name not filled:\n%s", on)
	}
	if !strings.Contains(string(on), "\n    extraGroups = [ \"networkmanager\" \"wheel\" ];") {
		t.Errorf("wheel not enabled:\n%s", on)
	}

	off, err := UserAccount("ana", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(off), "# extraGroups = [ \"networkmanager\" \"wheel\" ];") {
		t.Errorf("wheel not commented:\n%s", off)
	}
	if strings.Contains(string(off), "packages") {
		t.Errorf("account.nix must not hold the package list:\n%s", off)
	}
}

func TestWriteUser(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "modules/users/ana")
	if err := WriteUser(dir, "ana", []byte("ACCOUNT")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "account.nix")); got != "ACCOUNT" {
		t.Errorf("account.nix = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "packages.nix")); !strings.Contains(got, "users.users.ana.packages") {
		t.Errorf("packages.nix = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "default.nix")); !strings.Contains(got, "./account.nix") || !strings.Contains(got, "./packages.nix") {
		t.Errorf("default.nix = %q", got)
	}
	if err := WriteUser(dir, "ana", []byte("AGAIN")); err == nil {
		t.Error("second WriteUser into an existing unit must fail")
	}
}

func TestUserTemplates_Parse(t *testing.T) {
	skipIfNoNix(t)
	dir := filepath.Join(t.TempDir(), "ana")
	account, err := UserAccount("ana", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteUser(dir, "ana", account); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		assertParses(t, filepath.Join(dir, e.Name()))
	}
}

func assertParses(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Errorf("%s does not parse: %v\n%s", path, err, out)
	}
}
