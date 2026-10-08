package ui

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProgressPlainPrintsEveryLine(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf)
	p.Start()
	p.Printf("Ensuring %s...\n", "x")
	p.Changef("  created: %s", "y")
	p.Warnf("%s", "w")
	p.Errf("%s: %s", "f", "e")
	p.Done("preflight")
	p.Stop()
	want := "Ensuring x...\n  created: y\nWarning: w\nError: f: e\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestProgressLiveRewritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{w: &buf, term: true, pal: Colored}
	p.Start()
	p.Printf("  step one\n")
	time.Sleep(2 * spinnerInterval)
	p.Changef("  moved: %s", "a")
	p.Warnf("careful")
	p.Errf("broken")
	p.Printf("step two\n")
	time.Sleep(2 * spinnerInterval)
	p.Done("preflight")
	p.Stop()

	out := buf.String()
	for _, want := range []string{
		clearLine,
		"step one" + Colored.reset + wrapOn,
		"step two",
		clearLine + "  moved: a" + Colored.reset + "\n",
		Colored.yellow + warnPrefix + "careful" + Colored.reset + "\n",
		Colored.red + errorPrefix + "broken" + Colored.reset + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%q", want, out)
		}
	}
	if strings.Contains(out, "step one\n") {
		t.Errorf("a status line was printed with a newline:\n%q", out)
	}
	wantEnd := clearLine + Colored.green + donePrefix + "preflight" + Colored.reset + "\n"
	if !strings.HasSuffix(out, wantEnd) {
		t.Errorf("output does not end with %q:\n%q", wantEnd, out)
	}
}

func TestProgressStopClearsAndIsFinal(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{w: &buf, term: true}
	p.Start()
	p.Printf("working\n")
	time.Sleep(2 * spinnerInterval)
	p.Stop()
	if !strings.HasSuffix(buf.String(), clearLine) {
		t.Fatalf("Stop did not clear the line:\n%q", buf.String())
	}
	n := buf.Len()
	time.Sleep(2 * spinnerInterval)
	p.Stop()
	if buf.Len() != n {
		t.Errorf("output written after Stop:\n%q", buf.String()[n:])
	}
}

func TestProgressStopWithoutStart(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{w: &buf, term: true}
	p.Stop()
	if buf.Len() != 0 {
		t.Errorf("Stop on a never-started Progress wrote %q", buf.String())
	}
}

func TestProgressLiveNoColor(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{w: &buf, term: true}
	p.Start()
	p.Printf("step\n")
	time.Sleep(2 * spinnerInterval)
	p.Done("preflight")
	if strings.Contains(buf.String(), "\x1b[38") {
		t.Errorf("color codes with the zero Palette:\n%q", buf.String())
	}
	if !strings.HasSuffix(buf.String(), donePrefix+"preflight\n") {
		t.Errorf("missing done line:\n%q", buf.String())
	}
}

func TestProgressPlainHasNoEscapes(t *testing.T) {
	var buf bytes.Buffer
	p := NewProgress(&buf)
	p.Start()
	p.Printf("a\n")
	p.Changef("c")
	p.Warnf("b")
	p.Done("preflight")
	if strings.ContainsAny(buf.String(), "\x1b\r") {
		t.Errorf("plain output has escapes:\n%q", buf.String())
	}
}

func TestErrorPlain(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "err")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	Error(f, errors.New("boom"))
	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "Error: boom\n" {
		t.Errorf("got %q", got)
	}
}
