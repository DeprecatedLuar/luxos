package commands

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

func TestSyncUsers_OnlyBuiltUsers(t *testing.T) {
	h := userHost(t, false, "users/ana")
	if err := syncUsers(ui.NewProgress(io.Discard), h); err != nil {
		t.Fatal(err)
	}
	ana, _ := h.Find("ana")
	bob, _ := h.Find("bob")
	x, _ := h.Find("x")
	if _, err := os.Stat(filepath.Join(ana.Abs, "account.nix")); err != nil {
		t.Errorf("selected ana: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bob.Abs, "account.nix")); !os.IsNotExist(err) {
		t.Error("unselected bob got account.nix")
	}
	if _, err := os.Stat(filepath.Join(x.Abs, "account.nix")); !os.IsNotExist(err) {
		t.Error("non-user unit got account.nix")
	}
}

func TestSyncUsers_Shadow(t *testing.T) {
	h := userHost(t, true)
	if err := syncUsers(ui.NewProgress(io.Discard), h); err != nil {
		t.Fatal(err)
	}
	bob, _ := h.Find("bob")
	if _, err := os.Stat(filepath.Join(bob.Abs, "account.nix")); err != nil {
		t.Errorf("shadow bob: %v", err)
	}
	hidden := filepath.Join(h.ModulesDir, "users", "bob", "account.nix")
	if _, err := os.Stat(hidden); !os.IsNotExist(err) {
		t.Error("hidden original got account.nix")
	}
}

// userHost loads a host with shared users ana and bob and a non-user unit x,
// selecting the shared units named in selected. With shadowBob, a host unit
// bob shadows users/bob and is selected as ./bob.
func userHost(t *testing.T, shadowBob bool, selected ...string) *modules.Host {
	t.Helper()
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostDir := filepath.Join(root, "hosts", "box")
	files := []string{
		filepath.Join(modulesDir, "users", "ana", "default.nix"),
		filepath.Join(modulesDir, "users", "bob", "default.nix"),
		filepath.Join(modulesDir, "desktop", "x", "default.nix"),
	}
	lines := ""
	for _, name := range selected {
		lines += "    ../" + name + "\n"
	}
	if shadowBob {
		files = append(files, filepath.Join(hostDir, "modules", "bob", "default.nix"))
		lines += "    ./bob\n"
	}
	vendored, err := templates.File("user/default.nix")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte("{ }")
		if filepath.Base(filepath.Dir(filepath.Dir(f))) != "desktop" {
			data = vendored
		}
		if err := os.WriteFile(f, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(hostDir, "modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	sel := "{ ... }:\n{\n  imports = [\n" + lines + "  ];\n}\n"
	if err := os.WriteFile(filepath.Join(hostDir, "modules", "default.nix"), []byte(sel), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := modules.Load(modulesDir, hostDir, "")
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestCheckPasswords_OnlyBuiltUsers(t *testing.T) {
	h := userHost(t, false, "users/ana")
	err := checkPasswords(ui.NewProgress(io.Discard), h, t.TempDir(), os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "1 user") {
		t.Fatalf("err = %v, want one failing user", err)
	}
	var buf strings.Builder
	err = checkPasswords(ui.NewProgress(&buf), h, t.TempDir(), os.Getuid())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(buf.String(), "luxos passwd ana") {
		t.Errorf("output %q does not name luxos passwd ana", buf.String())
	}
	if strings.Contains(buf.String(), "bob") {
		t.Errorf("unselected bob was checked: %s", buf.String())
	}
}

// pullAna makes the shared unit x pull users/ana through luxos.modules.
func pullAna(t *testing.T, h *modules.Host) {
	t.Helper()
	x, _ := h.Find("x")
	pull := "{ luxos, ... }:\n{\n  imports = luxos.modules [ \"ana\" ];\n}\n"
	if err := os.WriteFile(filepath.Join(x.Abs, "default.nix"), []byte(pull), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPasswords_PulledUser(t *testing.T) {
	skipIfNoNix(t)
	h := userHost(t, false, "desktop/x")
	pullAna(t, h)
	var buf strings.Builder
	err := checkPasswords(ui.NewProgress(&buf), h, t.TempDir(), os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "1 user") {
		t.Fatalf("err = %v, want one failing user", err)
	}
	if !strings.Contains(buf.String(), "luxos passwd ana") {
		t.Errorf("pulled ana not checked: %s", buf.String())
	}
	if strings.Contains(buf.String(), "bob") {
		t.Errorf("unbuilt bob was checked: %s", buf.String())
	}
}

func TestBuiltUsers(t *testing.T) {
	skipIfNoNix(t)
	h := userHost(t, true, "desktop/x")
	pullAna(t, h)
	users, err := builtUsers(h)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range users {
		got = append(got, m.Name+"="+m.Path)
	}
	want := "bob=host/bob,ana=users/ana"
	if strings.Join(got, ",") != want {
		t.Errorf("built users = %v, want %s", got, want)
	}
}

func TestCheckPasswords_ReportsEveryFailure(t *testing.T) {
	h := userHost(t, false, "users/ana", "users/bob")
	var buf strings.Builder
	err := checkPasswords(ui.NewProgress(&buf), h, t.TempDir(), os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "2 user") {
		t.Fatalf("err = %v, want 2 failing users", err)
	}
	for _, name := range []string{"ana", "bob"} {
		if !strings.Contains(buf.String(), "luxos passwd "+name) {
			t.Errorf("output does not name %s: %s", name, buf.String())
		}
	}
}

func TestCheckPasswords_AllPass(t *testing.T) {
	for _, bin := range []string{"age", "age-keygen", "mkpasswd"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
	h := userHost(t, false, "users/ana", "users/bob")
	keys := t.TempDir()
	for _, name := range []string{"ana", "bob"} {
		secret, err := password.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		pub, err := password.PublicKey(secret)
		if err != nil {
			t.Fatal(err)
		}
		hash, err := password.Hash("pw")
		if err != nil {
			t.Fatal(err)
		}
		enc, err := password.Encrypt(hash, pub)
		if err != nil {
			t.Fatal(err)
		}
		m, _ := h.Find(name)
		if err := os.WriteFile(filepath.Join(m.Abs, password.File), enc, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := password.WriteKey(keys, name, secret); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkPasswords(ui.NewProgress(io.Discard), h, keys, os.Getuid()); err != nil {
		t.Errorf("all users pass: %v", err)
	}
}

func TestRequirePassword(t *testing.T) {
	h := userHost(t, false)
	ana, _ := h.Find("ana")
	if err := requirePassword(ana); err == nil || !strings.Contains(err.Error(), "luxos passwd ana") {
		t.Errorf("err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ana.Abs, password.File), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requirePassword(ana); err != nil {
		t.Errorf("with a password file: %v", err)
	}
	x, _ := h.Find("x")
	if err := requirePassword(x); err != nil {
		t.Errorf("non-user unit: %v", err)
	}
}

func TestRequirePassword_Shadow(t *testing.T) {
	h := userHost(t, true)
	bob, ok := h.Find("bob")
	if !ok || bob.Shadows == "" {
		t.Fatalf("bob = %+v, want a shadow", bob)
	}
	if err := requirePassword(bob); err == nil {
		t.Error("shadow without a password file passed")
	}
	if err := os.WriteFile(filepath.Join(bob.Abs, password.File), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requirePassword(bob); err != nil {
		t.Errorf("shadow with its own password file: %v", err)
	}
}
