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
	for _, f := range []string{"default.nix", "account.nix", "options.nix", "modules/packages.nix", "modules/default.nix"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "packages.nix")); !os.IsNotExist(err) {
		t.Error("packages.nix written at the unit root")
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
	if len(changes) != 5 || changes[0].Action != ActionCreated ||
		changes[3] != (SyncChange{Path: UserPackagesFile, Action: ActionCreated}) ||
		changes[4] != (SyncChange{Path: UserSelectionFile, Action: ActionCreated}) {
		t.Errorf("first sync = %+v", changes)
	}
	sel, err := os.ReadFile(UserSelectionPath(dir))
	if err != nil || !strings.Contains(string(sel), "./packages.nix") {
		t.Errorf("selection = %q, %v; want it to select ./packages.nix", sel, err)
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
	for _, f := range []string{"default.nix", "account.nix", "options.nix"} {
		if fi, _ := os.Stat(filepath.Join(dir, f)); fi.Mode().Perm() != 0o444 {
			t.Errorf("%s mode %v", f, fi.Mode().Perm())
		}
	}
	if changes, _ := SyncUser(dir); len(changes) != 0 {
		t.Errorf("third sync = %+v, want nothing", changes)
	}
}

func TestSyncUser_KeepsExistingModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	changes, err := SyncUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 4 || changes[3] != (SyncChange{Path: UserSelectionFile, Action: ActionCreated}) {
		t.Errorf("sync = %+v, want the vendored files and an empty selection", changes)
	}
	sel, err := os.ReadFile(UserSelectionPath(dir))
	if err != nil || strings.Contains(string(sel), "./") {
		t.Errorf("selection = %q, %v; want an empty imports list", sel, err)
	}
	if _, err := os.Stat(filepath.Join(dir, UserPackagesFile)); !os.IsNotExist(err) {
		t.Error("starter written into an existing modules/")
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
	for _, want := range []string{acct, "move your values into " + filepath.Join(dir, "modules"), "delete " + acct} {
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

func TestSyncUser_ReplacesHandWrittenDefault(t *testing.T) {
	dir := t.TempDir()
	def := filepath.Join(dir, "default.nix")
	hand := []byte("{ ... }:\n{\n  imports = [ ./account.nix ./hjem.nix ];\n}\n")
	if err := os.WriteFile(def, hand, 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := SyncUser(dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := templates.File("user/default.nix")
	if got, _ := os.ReadFile(def); !bytes.Equal(got, want) {
		t.Error("default.nix not replaced by the luxos file")
	}
	if kept, _ := os.ReadFile(def + ".old"); !bytes.Equal(kept, hand) {
		t.Errorf("hand-written default.nix not kept as default.nix.old: %q", kept)
	}
	if len(changes) == 0 || changes[0] != (SyncChange{Path: "default.nix", Action: ActionReplaced}) {
		t.Errorf("changes = %+v", changes)
	}
	if again, _ := SyncUser(dir); len(again) != 0 {
		t.Errorf("second sync = %+v, want nothing", again)
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
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".nix") {
			assertParses(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertParses(t *testing.T, path string) {
	t.Helper()
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Errorf("%s does not parse: %v\n%s", path, err, out)
	}
}

func TestSyncUser_KeepsExistingSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "{ ... }:\n{\n  # mine\n  imports = [\n    ./a.nix\n  ];\n}\n"
	if err := os.WriteFile(UserSelectionPath(dir), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncUser(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(UserSelectionPath(dir)); string(got) != mine {
		t.Errorf("selection rewritten: %q", got)
	}
}

func TestUserScope(t *testing.T) {
	if got := UserScope("luar"); got != "users/luar/modules" {
		t.Errorf("UserScope = %q", got)
	}
	if got := LineOf(UserScope("luar"), "users/luar/modules/ui/a.nix"); got != "./ui/a.nix" {
		t.Errorf("LineOf = %q", got)
	}
	if got := PathOf(UserScope("luar"), "./a.nix"); got != "users/luar/modules/a.nix" {
		t.Errorf("PathOf = %q", got)
	}
}

func TestCreateUserSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CreateUserSelection(dir, []string{"./a.nix"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(UserSelectionPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	want, err := templates.Render(selectionTemplate, []string{"./a.nix"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("selection = %q, want %q", got, want)
	}
}
