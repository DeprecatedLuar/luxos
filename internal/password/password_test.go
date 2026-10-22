package password

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func skipIfNoTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{ageBin, ageKeygenBin, mkpasswdBin} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}
}

func TestHash_Yescrypt(t *testing.T) {
	skipIfNoTools(t)
	h, err := Hash("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$y$") || strings.ContainsAny(h, "\n ") {
		t.Errorf("hash = %q, want a single-line $y$ hash", h)
	}
}

func TestHash_EmptyRefused(t *testing.T) {
	if _, err := Hash(""); !errors.Is(err, ErrEmpty) {
		t.Errorf("err = %v, want ErrEmpty", err)
	}
}

func TestKeyRoundTrip(t *testing.T) {
	skipIfNoTools(t)
	secret, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, secretPrefix) || strings.Contains(secret, "\n") {
		t.Fatalf("secret = %q", secret)
	}
	pub, err := PublicKey(secret)
	if err != nil || !strings.HasPrefix(pub, "age1") {
		t.Fatalf("pub = %q, %v", pub, err)
	}
	enc, err := Encrypt("$y$fake", pub)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(enc), "-----BEGIN AGE ENCRYPTED FILE-----") {
		t.Errorf("not armored: %q", enc)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, File)
	keyFile := filepath.Join(dir, "key")
	if err := os.WriteFile(file, enc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte(secret+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := Decrypt(file, keyFile); err != nil {
		t.Errorf("Decrypt with the right key: %v", err)
	}

	other, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	otherFile := filepath.Join(dir, "other")
	if err := os.WriteFile(otherFile, []byte(other+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := Decrypt(file, otherFile); err == nil {
		t.Error("Decrypt with the wrong key succeeded")
	}
}

func TestPublicKey_Garbage(t *testing.T) {
	skipIfNoTools(t)
	if _, err := PublicKey("garbage"); err == nil {
		t.Error("PublicKey(garbage) succeeded")
	}
}

func TestSecretLine(t *testing.T) {
	const key = "AGE-SECRET-KEY-1ABC"
	for _, in := range []string{
		key,
		key + "\n",
		"  " + key + "  \n",
		"# created: 2026-10-16T09:05:39-03:00\n# public key: age1xyz\n" + key + "\n",
	} {
		got, err := SecretLine(in)
		if err != nil || got != key {
			t.Errorf("SecretLine(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "age1xyz", key + "\n" + key + "2\n"} {
		if _, err := SecretLine(in); err == nil {
			t.Errorf("SecretLine(%q) succeeded", in)
		}
	}
}

func TestSealDecryptsOnlyWithItsKey(t *testing.T) {
	skipIfNoTools(t)
	dir := t.TempDir()
	secret, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Seal("$y$hash", secret)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, File)
	keyFile := filepath.Join(dir, "key")
	otherFile := filepath.Join(dir, "other")
	for path, data := range map[string]string{file: string(enc), keyFile: secret + "\n", otherFile: other + "\n"} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Decrypt(file, keyFile); err != nil {
		t.Errorf("Decrypt with its key: %v", err)
	}
	if err := Decrypt(file, otherFile); err == nil {
		t.Error("Decrypt with another key succeeded")
	}
	if _, err := Seal("$y$hash", "garbage"); err == nil {
		t.Error("Seal to a non-key succeeded")
	}
}
