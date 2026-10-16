package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

func pipeWith(t *testing.T, input string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() { r.Close() })
	return r
}

func TestAskSecret_ReadsLinesOneAtATime(t *testing.T) {
	in := pipeWith(t, "first\nsecond\n")
	var out strings.Builder
	a, err := AskSecret(in, &out, "Password: ")
	if err != nil || a != "first" {
		t.Fatalf("first = %q, %v", a, err)
	}
	b, err := AskSecret(in, &out, "Again: ")
	if err != nil || b != "second" {
		t.Fatalf("second = %q, %v", b, err)
	}
	if out.String() != "Password: Again: " {
		t.Errorf("prompts = %q", out.String())
	}
}

func TestAskSecret_KeepsSpaces(t *testing.T) {
	got, err := AskSecret(pipeWith(t, " a b \n"), io.Discard, "")
	if err != nil || got != " a b " {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestAskSecret_EndOfInput(t *testing.T) {
	if _, err := AskSecret(pipeWith(t, ""), io.Discard, ""); err != io.EOF {
		t.Errorf("err = %v, want io.EOF", err)
	}
	got, err := AskSecret(pipeWith(t, "last"), io.Discard, "")
	if err != nil || got != "last" {
		t.Errorf("unterminated line = %q, %v", got, err)
	}
}

func TestIsTTY_Pipe(t *testing.T) {
	if term, err := IsTTY(pipeWith(t, "")); err != nil || term {
		t.Errorf("pipe: %v, %v", term, err)
	}
}
