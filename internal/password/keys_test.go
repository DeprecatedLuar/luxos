package password

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	if err == nil || !strings.Contains(err.Error(), "luxos passwd luar") {
		t.Errorf("no password file: %v", err)
	}

	secret, _ := NewKey()
	pub, _ := PublicKey(secret)
	enc, _ := Encrypt("$y$fake", pub)
	if err := os.WriteFile(filepath.Join(unit, File), enc, 0o644); err != nil {
		t.Fatal(err)
	}
	err = Check(unit, keys, "luar", os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "luxos key add luar") {
		t.Errorf("no key: %v", err)
	}

	other, _ := NewKey()
	if err := WriteKey(keys, "luar", other); err != nil {
		t.Fatal(err)
	}
	err = Check(unit, keys, "luar", os.Getuid())
	if err == nil || !strings.Contains(err.Error(), "luxos key add luar") {
		t.Errorf("wrong key: %v", err)
	}

	if err := WriteKey(keys, "luar", secret); err != nil {
		t.Fatal(err)
	}
	if err := Check(unit, keys, "luar", os.Getuid()); err != nil {
		t.Errorf("valid setup: %v", err)
	}
}
