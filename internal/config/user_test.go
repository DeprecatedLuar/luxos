package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

func TestWriteUser(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "users", "ana")
	if err := WriteUser(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"default.nix", "account.nix", "options.nix", "packages.nix"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if err := WriteUser(dir); err == nil {
		t.Error("WriteUser over an existing unit succeeded")
	}
}

func TestSyncUser_RestoresAndLocks(t *testing.T) {
	dir := t.TempDir()
	changes, err := SyncUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Action != ActionCreated {
		t.Errorf("first sync = %+v", changes)
	}

	acct := filepath.Join(dir, "account.nix")
	if err := os.Chmod(acct, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acct, []byte(luxosPropertyHeader+"\n{ }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err = SyncUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != (SyncChange{Path: "account.nix", Action: ActionRestored}) {
		t.Errorf("second sync = %+v", changes)
	}
	want, _ := templates.File("user/account.nix")
	got, _ := os.ReadFile(acct)
	if string(got) != string(want) {
		t.Error("account.nix not restored")
	}
	for _, f := range []string{"account.nix", "options.nix"} {
		if fi, _ := os.Stat(filepath.Join(dir, f)); fi.Mode().Perm() != 0o444 {
			t.Errorf("%s mode %v", f, fi.Mode().Perm())
		}
	}
	if changes, _ := SyncUser(dir); len(changes) != 0 {
		t.Errorf("third sync = %+v, want nothing", changes)
	}
}

func TestSyncUser_RefusesHandWritten(t *testing.T) {
	dir := t.TempDir()
	acct := filepath.Join(dir, "account.nix")
	hand := []byte("{ ... }:\n{\n  users.users.ana.openssh.authorizedKeys.keys = [ \"ssh-ed25519 AAAA\" ];\n}\n")
	if err := os.WriteFile(acct, hand, 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := SyncUser(dir)
	if err == nil {
		t.Fatalf("hand-written account.nix replaced: %+v", changes)
	}
	for _, want := range []string{acct, "move your values to another file of the unit", "delete " + acct} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	got, _ := os.ReadFile(acct)
	if !bytes.Equal(got, hand) {
		t.Error("hand-written account.nix changed")
	}
	if fi, _ := os.Stat(acct); fi.Mode().Perm() != 0o644 {
		t.Errorf("hand-written account.nix mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "options.nix")); !os.IsNotExist(err) {
		t.Error("options.nix written for a refused unit")
	}
}

func TestUserGroups(t *testing.T) {
	want := "{\n  ana.groups = [ \"wheel\" \"networkmanager\" ];\n}\n"
	if got := string(UserGroups("ana")); got != want {
		t.Errorf("got %q", got)
	}
}

func TestUserTemplates_Parse(t *testing.T) {
	skipIfNoNix(t)
	dir := filepath.Join(t.TempDir(), "ana")
	if err := WriteUser(dir); err != nil {
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
