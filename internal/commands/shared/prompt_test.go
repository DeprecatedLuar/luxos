package shared

import (
	"os"
	"testing"
)

func confirmWith(t *testing.T, input string) bool {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	ok, err := ConfirmHardwareOff()
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestConfirmHardwareOff(t *testing.T) {
	if !confirmWith(t, "y\n") {
		t.Fatal("y should confirm")
	}
	if confirmWith(t, "n\n") {
		t.Fatal("n should not confirm")
	}
	if confirmWith(t, "") {
		t.Fatal("closed stdin should not confirm")
	}
}
