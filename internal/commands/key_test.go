package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

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

func TestIsKeyComment(t *testing.T) {
	for line, want := range map[string]bool{
		"# created: x": true, "  # public key: age1": true, "AGE-SECRET-KEY-1ABC": false, "": false, "garbage": false,
	} {
		if got := isKeyComment(line); got != want {
			t.Errorf("isKeyComment(%q) = %v", line, got)
		}
	}
}

func TestOneUserMisuse(t *testing.T) {
	assertMisuse(t, func() error { _, err := oneUser(nil); return err }, "want exactly one user", "luxos key <verb> <user>")
}

func TestKeyBare(t *testing.T) {
	assertMisuse(t, func() error { return Key(nil) }, "luxos key <verb> <user>")
}

func TestKeyHelpFlag(t *testing.T) {
	assertHelp(t, func() error { return Key([]string{"--help"}) }, "luxos key <verb> <user>")
}
