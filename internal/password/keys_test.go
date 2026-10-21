package password

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

func TestWriteKeyModes(t *testing.T) {
	skipIfNoTools(t)
	keys := filepath.Join(t.TempDir(), "keys")
	secret, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteKey(keys, "luar", secret); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(keys); fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(KeyPath(keys, "luar")); fi.Mode().Perm() != 0o400 {
		t.Errorf("key mode %v", fi.Mode().Perm())
	}
	got, err := ReadKey(keys, "luar", os.Getuid())
	if err != nil || got != secret {
		t.Errorf("ReadKey = %q, %v", got, err)
	}
	// Rewriting over a 0400 key works.
	if err := WriteKey(keys, "luar", secret); err != nil {
		t.Errorf("second WriteKey: %v", err)
	}
}

func TestCheckKey(t *testing.T) {
	skipIfNoTools(t)
	keys := t.TempDir()
	if err := CheckKey(keys, "luar", os.Getuid()); !errors.Is(err, ErrNoKey) {
		t.Errorf("missing key: %v, want ErrNoKey", err)
	}
	if err := os.WriteFile(KeyPath(keys, "luar"), []byte("AGE-SECRET-KEY-1X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckKey(keys, "luar", os.Getuid()); err == nil || !strings.Contains(err.Error(), "0400") {
		t.Errorf("wrong mode: %v", err)
	}
	if err := os.Chmod(KeyPath(keys, "luar"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := CheckKey(keys, "luar", os.Getuid()+1); err == nil {
		t.Error("wrong owner accepted")
	}
	if err := CheckKey(keys, "luar", os.Getuid()); err == nil {
		t.Error("corrupt key accepted")
	}
}

func TestUsersAndRemove(t *testing.T) {
	skipIfNoTools(t)
	keys := filepath.Join(t.TempDir(), "keys")
	if got, err := Users(keys); err != nil || len(got) != 0 {
		t.Errorf("Users of a missing folder = %v, %v", got, err)
	}
	for _, u := range []string{"zed", "ana"} {
		s, err := NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if err := WriteKey(keys, u, s); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := Users(keys); !reflect.DeepEqual(got, []string{"ana", "zed"}) {
		t.Errorf("Users = %v", got)
	}
	if err := RemoveKey(keys, "zed"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveKey(keys, "zed"); !errors.Is(err, ErrNoKey) {
		t.Errorf("second RemoveKey: %v, want ErrNoKey", err)
	}
}

func TestCheck(t *testing.T) {
	skipIfNoTools(t)
	unit := t.TempDir()
	keys := filepath.Join(t.TempDir(), "keys")

	err := Check(unit, keys, "luar", os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "luxos user passwd luar") {
		t.Errorf("no password file: %v", err)
	}

	secret, _ := NewKey()
	pub, _ := PublicKey(secret)
	enc, _ := Encrypt("$y$fake", pub)
	if err := os.WriteFile(filepath.Join(unit, File), enc, 0o644); err != nil {
		t.Fatal(err)
	}
	err = Check(unit, keys, "luar", os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "luxos user key add luar") {
		t.Errorf("no key: %v", err)
	}

	other, _ := NewKey()
	if err := WriteKey(keys, "luar", other); err != nil {
		t.Fatal(err)
	}
	err = Check(unit, keys, "luar", os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "luxos user key add luar") {
		t.Errorf("wrong key: %v", err)
	}
	ageErr := Decrypt(filepath.Join(unit, File), KeyPath(keys, "luar"))
	if ageErr == nil || !strings.Contains(err.Error(), ageErr.Error()) {
		t.Errorf("wrong key: %v does not carry age's error %v", err, ageErr)
	}

	if err := WriteKey(keys, "luar", secret); err != nil {
		t.Fatal(err)
	}
	if err := Check(unit, keys, "luar", os.Getuid()); err != nil {
		t.Errorf("valid setup: %v", err)
	}
}

func TestRepairKey(t *testing.T) {
	skipIfNoTools(t)
	keys := t.TempDir()
	uid := os.Getuid()
	if _, err := RepairKey(keys, "luar", uid); !errors.Is(err, ErrNoKey) {
		t.Errorf("missing key: %v, want ErrNoKey", err)
	}

	secret, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	path := KeyPath(keys, "luar")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := RepairKey(keys, "luar", uid)
	if err != nil || got != secret {
		t.Fatalf("valid key, wrong mode: %q, %v", got, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o400 {
		t.Errorf("mode %v, want 0400", fi.Mode().Perm())
	}
	if _, err := ReadKey(keys, "luar", uid); err != nil {
		t.Errorf("repaired key fails ReadKey: %v", err)
	}

	if uid != 0 {
		if _, err := RepairKey(keys, "luar", uid+1); err == nil || errors.Is(err, ErrNotKey) || !strings.Contains(err.Error(), "owner") {
			t.Errorf("unfixable owner: %v, want an owner error", err)
		}
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RepairKey(keys, "luar", uid); !errors.Is(err, ErrNotKey) {
		t.Errorf("garbage: %v, want ErrNotKey", err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("garbage was repaired: mode %v", fi.Mode().Perm())
	}
}

func TestMissingToolIsNotBadKey(t *testing.T) {
	saved := ageKeygenBin
	ageKeygenBin = "luxos-test-missing-age-keygen"
	t.Cleanup(func() { ageKeygenBin = saved })

	keys, unit := t.TempDir(), t.TempDir()
	uid := os.Getuid()
	if err := os.WriteFile(KeyPath(keys, "luar"), []byte(secretPrefix+"X\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unit, File), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := RepairKey(keys, "luar", uid)
	if errors.Is(err, ErrNotKey) || !shell.NotFound(err) {
		t.Errorf("RepairKey: %v, want a missing-program error", err)
	}
	err = Check(unit, keys, "luar", uid)
	if !shell.NotFound(err) || strings.Contains(err.Error(), "key add") {
		t.Errorf("Check: %v, want a missing-program error without a key fix", err)
	}
}
