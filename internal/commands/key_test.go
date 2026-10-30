package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

func TestReadKeyInput_PipedFullKeygenOutput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("# created: 2026-10-16T09:05:39-03:00\n# public key: age1xyz\nAGE-SECRET-KEY-1ABC  \n")
	w.Close()
	defer r.Close()
	got, err := readKeyInput(r, io.Discard)
	if err != nil || got != "AGE-SECRET-KEY-1ABC" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestReadKeyInput_EmptyPipe(t *testing.T) {
	r, w, _ := os.Pipe()
	w.Close()
	defer r.Close()
	if _, err := readKeyInput(r, io.Discard); err == nil {
		t.Error("empty input accepted")
	}
}

func TestOneUser(t *testing.T) {
	cases := []struct {
		name string
		args []string
		ok   bool
	}{
		{"plain", []string{"luar"}, true},
		{"dashed", []string{"luar-2"}, true},
		{"none", nil, false},
		{"two", []string{"a", "b"}, false},
		{"empty", []string{""}, false},
		{"dot", []string{"."}, false},
		{"dotdot", []string{".."}, false},
		{"slash", []string{"a/b"}, false},
		{"traversal", []string{"../etc"}, false},
		{"absolute", []string{"/etc/passwd"}, false},
	}
	for _, c := range cases {
		_, err := oneUser(c.args)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestKeyGet(t *testing.T) {
	if uid := os.Getuid(); uid != keyOwnerUID {
		t.Skipf("keys must be owned by uid %d, running as %d", keyOwnerUID, uid)
	}
	dir := t.TempDir()
	p := paths.Paths{Keys: dir}
	secret, err := password.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := password.WriteKey(dir, "luar", secret); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := keyGet(p, []string{"luar"}, &out); err != nil || out.String() != secret+"\n" {
		t.Errorf("got %q, %v", out.String(), err)
	}
	if err := keyGet(p, []string{"nobody"}, io.Discard); !errors.Is(err, password.ErrNoKey) {
		t.Errorf("missing key: err = %v", err)
	}
}

func TestOneUserMisuse(t *testing.T) {
	assertMisuse(t, func() error { _, err := oneUser(nil); return err }, "want exactly one user", "key add <user>")
}

func TestKeyBare(t *testing.T) {
	assertMisuse(t, func() error { return Key(nil) }, "key add <user>")
}

func TestKeyHelpFlag(t *testing.T) {
	assertHelp(t, func() error { return Key([]string{"--help"}) }, "key add <user>")
}

func TestFindUserUnit(t *testing.T) {
	modulesDir := filepath.Join(t.TempDir(), "modules")
	if _, ok, err := findUserUnit(modulesDir, testKeyUser); ok || err != nil {
		t.Fatalf("missing modules dir: ok=%v err=%v, want false, nil", ok, err)
	}
	if err := config.WriteUser(filepath.Join(modulesDir, "users", testKeyUser)); err != nil {
		t.Fatal(err)
	}
	m, ok, err := findUserUnit(modulesDir, testKeyUser)
	if err != nil || !ok || !strings.HasSuffix(m.Abs, filepath.Join("users", testKeyUser)) {
		t.Errorf("found = %v, %v, %q", ok, err, m.Abs)
	}
}

func TestStoreKey_ChecksPasswordFile(t *testing.T) {
	skipIfNoAge(t)
	root := t.TempDir()
	p := paths.Paths{Modules: filepath.Join(root, "modules"), Keys: filepath.Join(root, "keys")}
	unit := filepath.Join(p.Modules, "users", testKeyUser)
	if err := config.WriteUser(unit); err != nil {
		t.Fatal(err)
	}
	right, err := password.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := password.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := password.Seal("hash", right)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFile(filepath.Join(unit, password.File), enc); err != nil {
		t.Fatal(err)
	}
	uid := os.Getuid()
	keyFile := password.KeyPath(p.Keys, testKeyUser)

	err = storeKey(p, testKeyUser, wrong, uid)
	if err == nil || !strings.Contains(err.Error(), "does not decrypt") || !strings.Contains(err.Error(), "the key was not stored") {
		t.Fatalf("wrong key: err = %v", err)
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Errorf("wrong key left on disk: %v", err)
	}
	if err := storeKey(p, testKeyUser, right, uid); err != nil {
		t.Fatalf("right key: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("right key not stored: %v", err)
	}
}

func TestStoreKey_NoUnitStoresUnchecked(t *testing.T) {
	skipIfNoAge(t)
	root := t.TempDir()
	p := paths.Paths{Modules: filepath.Join(root, "modules"), Keys: filepath.Join(root, "keys")}
	secret, err := password.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := storeKey(p, testKeyUser, secret, os.Getuid()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(password.KeyPath(p.Keys, testKeyUser)); err != nil {
		t.Errorf("key not stored: %v", err)
	}
}

func TestKeyList_NamesOnly(t *testing.T) {
	keys := t.TempDir()
	for _, u := range []string{"luar", "alice"} {
		if err := password.WriteKey(keys, u, secretKeyPrefix+"X"); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := keyList(keys, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "alice\nluar\n" {
		t.Errorf("key list = %q, want %q", out.String(), "alice\nluar\n")
	}
}
