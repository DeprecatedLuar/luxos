package commands

import (
	"bytes"
	"errors"
	"io"
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

func answers(lines ...string) func(string) (string, error) {
	return func(string) (string, error) {
		if len(lines) == 0 {
			return "", io.EOF
		}
		l := lines[0]
		lines = lines[1:]
		return l, nil
	}
}

func TestReadNewPassword(t *testing.T) {
	if pw, err := readNewPassword(answers("s3cret", "s3cret")); err != nil || pw != "s3cret" {
		t.Errorf("match = %q, %v", pw, err)
	}
	if _, err := readNewPassword(answers("a", "b")); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Errorf("mismatch: %v", err)
	}
	if _, err := readNewPassword(answers("", "")); !errors.Is(err, password.ErrEmpty) {
		t.Errorf("empty: %v", err)
	}
	if _, err := readNewPassword(answers("a")); !errors.Is(err, io.EOF) {
		t.Errorf("end of input: %v", err)
	}
}

func TestUserKey(t *testing.T) {
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not installed")
	}
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not installed")
	}
	uid := os.Getuid()
	dir := filepath.Join(t.TempDir(), "keys")

	var first bytes.Buffer
	pub, err := userKey(dir, testKeyUser, uid, &first)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), secretKeyPrefix) {
		t.Errorf("new key not printed: %q", first.String())
	}
	if _, err := os.Stat(password.KeyPath(dir, testKeyUser)); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	again, err := userKey(dir, testKeyUser, uid, &second)
	if err != nil {
		t.Fatal(err)
	}
	if again != pub || second.Len() != 0 {
		t.Errorf("reuse: pub %q vs %q, output %q", again, pub, second.String())
	}

	path := password.KeyPath(dir, testKeyUser)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(corruptKeyBody), 0o400); err != nil {
		t.Fatal(err)
	}
	var third bytes.Buffer
	if _, err := userKey(dir, testKeyUser, uid, &third); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(third.String(), "warning") || !strings.Contains(third.String(), secretKeyPrefix) {
		t.Errorf("corrupt key not replaced with warning: %q", third.String())
	}
	if strings.Contains(third.String(), "garbage") {
		t.Errorf("warning leaks key content: %q", third.String())
	}
	if _, err := password.ReadKey(dir, testKeyUser, uid); err != nil {
		t.Errorf("replacement unreadable: %v", err)
	}
}
