package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/password"
)

const (
	secretKeyPrefix = "AGE-SECRET-KEY-1"
	corruptKeyBody  = "garbage\n"
	testKeyUser     = "luar"
)

func skipIfNoAge(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"age", "age-keygen"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
}

func TestUserKey(t *testing.T) {
	skipIfNoAge(t)
	uid := os.Getuid()
	dir := filepath.Join(t.TempDir(), "keys")
	path := password.KeyPath(dir, testKeyUser)

	var warn bytes.Buffer
	secret, fresh, err := userKey(dir, testKeyUser, uid, &warn)
	if err != nil || !fresh || !strings.HasPrefix(secret, secretKeyPrefix) {
		t.Fatalf("missing key: %q, fresh %v, %v", secret, fresh, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("userKey stored the new key")
	}
	if warn.Len() != 0 {
		t.Errorf("missing key warned: %q", warn.String())
	}

	if err := password.WriteKey(dir, testKeyUser, secret); err != nil {
		t.Fatal(err)
	}
	again, fresh, err := userKey(dir, testKeyUser, uid, &warn)
	if err != nil || fresh || again != secret || warn.Len() != 0 {
		t.Errorf("reuse: %q fresh %v, %v, warn %q", again, fresh, err, warn.String())
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	kept, fresh, err := userKey(dir, testKeyUser, uid, &warn)
	if err != nil || fresh || kept != secret {
		t.Errorf("wrong-mode valid key not kept: fresh %v, %v", fresh, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o400 {
		t.Errorf("mode not repaired: %v", fi.Mode().Perm())
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(corruptKeyBody), 0o600); err != nil {
		t.Fatal(err)
	}
	replaced, fresh, err := userKey(dir, testKeyUser, uid, &warn)
	if err != nil || !fresh || replaced == secret {
		t.Fatalf("corrupt key: fresh %v, %v", fresh, err)
	}
	if !strings.Contains(warn.String(), "[WARNING]") {
		t.Errorf("corrupt key replaced without a warning: %q", warn.String())
	}
	if strings.Contains(warn.String(), "garbage") || strings.Contains(warn.String(), secret) || strings.Contains(warn.String(), replaced) {
		t.Errorf("warning leaks key content: %q", warn.String())
	}
}

func TestStorePassword(t *testing.T) {
	skipIfNoAge(t)
	uid := os.Getuid()
	keys := filepath.Join(t.TempDir(), "keys")
	file := filepath.Join(t.TempDir(), password.File)

	var warn bytes.Buffer
	secret, err := storePassword(keys, testKeyUser, file, "hash", uid, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, secretKeyPrefix) {
		t.Errorf("new key not returned: %q", secret)
	}
	if err := password.Decrypt(file, password.KeyPath(keys, testKeyUser)); err != nil {
		t.Errorf("stored key does not decrypt: %v", err)
	}

	secret, err = storePassword(keys, testKeyUser, file, "hash2", uid, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" || warn.Len() != 0 {
		t.Errorf("reused key returned or warned: %q %q", secret, warn.String())
	}
}

func TestStorePassword_WriteFailsStillReturnsKey(t *testing.T) {
	skipIfNoAge(t)
	keys := filepath.Join(t.TempDir(), "keys")
	file := filepath.Join(t.TempDir(), "missing", password.File)

	var warn bytes.Buffer
	secret, err := storePassword(keys, testKeyUser, file, "hash", os.Getuid(), &warn)
	if err == nil {
		t.Fatal("writing into a missing folder succeeded")
	}
	if !strings.HasPrefix(secret, secretKeyPrefix) {
		t.Errorf("stored key not returned after the failed write: %q", secret)
	}
	if _, err := os.Stat(password.KeyPath(keys, testKeyUser)); err != nil {
		t.Errorf("key not stored: %v", err)
	}
}

func TestStorePassword_EncryptFailsStoresNoKey(t *testing.T) {
	skipIfNoAge(t)
	keygen, err := exec.LookPath("age-keygen")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(keygen, filepath.Join(bin, "age-keygen")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	keys := filepath.Join(t.TempDir(), "keys")
	file := filepath.Join(t.TempDir(), password.File)
	var warn bytes.Buffer
	secret, err := storePassword(keys, testKeyUser, file, "hash", os.Getuid(), &warn)
	if err == nil {
		t.Fatal("encrypt without age succeeded")
	}
	if _, err := os.Stat(password.KeyPath(keys, testKeyUser)); !os.IsNotExist(err) {
		t.Error("a key was stored though encryption failed")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("a password file was written though encryption failed")
	}
	if secret != "" {
		t.Errorf("an unstored key was returned: %q", secret)
	}
}

func TestPasswdHelp(t *testing.T) {
	assertHelp(t, func() error { return Passwd([]string{"-h"}) }, "luxos user passwd <user>")
}

func TestPasswdNoArgs(t *testing.T) {
	assertMisuse(t, func() error { return Passwd(nil) }, "want exactly one user", "luxos user passwd <user>")
}
