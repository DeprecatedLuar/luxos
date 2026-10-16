package commands

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

func TestSyncUsers_SharedAndShadow(t *testing.T) {
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostDir := filepath.Join(root, "hosts", "box")
	for _, f := range []string{
		filepath.Join(modulesDir, "users", "ana", "default.nix"),
		filepath.Join(modulesDir, "users", "bob", "default.nix"),
		filepath.Join(modulesDir, "desktop", "x", "default.nix"),
		filepath.Join(hostDir, "modules", "bob", "default.nix"),
	} {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, []byte("{ }"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(hostDir, "modules", "default.nix"), []byte("{ ... }:\n{\n  imports = [\n  ];\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := modules.Load(modulesDir, hostDir, "")
	if err != nil {
		t.Fatal(err)
	}
	out := ui.NewProgress(io.Discard)
	if err := syncUsers(out, h); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{
		filepath.Join(modulesDir, "users", "ana"),
		filepath.Join(hostDir, "modules", "bob"),
	} {
		if _, err := os.Stat(filepath.Join(dir, "account.nix")); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
	}
	if _, err := os.Stat(filepath.Join(modulesDir, "desktop", "x", "account.nix")); !os.IsNotExist(err) {
		t.Error("non-user unit got account.nix")
	}
	if _, err := os.Stat(filepath.Join(modulesDir, "users", "bob", "account.nix")); !os.IsNotExist(err) {
		t.Error("hidden original got account.nix")
	}
}
