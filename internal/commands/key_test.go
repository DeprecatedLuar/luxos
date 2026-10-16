package commands

import (
	"io"
	"os"
	"testing"
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
		_, err := oneUser("add", c.args)
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}
