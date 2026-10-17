package ui

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestMessagesPlain(t *testing.T) {
	var buf bytes.Buffer
	Done(&buf, "saved")
	Warn(&buf, "careful")
	Error(&buf, errors.New("broken"))
	Text(&buf, "plain")
	Print(&buf, "block\n")
	want := "✓ saved\n[WARNING] careful\n[ERROR] broken\nplain\nblock\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestPaletteOfNonFileIsPlain(t *testing.T) {
	if PaletteOf(&bytes.Buffer{}) != (Palette{}) {
		t.Error("a buffer got a colored palette")
	}
}

func TestPaletteNameKeepsColor(t *testing.T) {
	got := Colored.Name("luar")
	if got != codeUnderline+"luar"+codeNoUnderline {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, codeReset) {
		t.Errorf("Name resets the surrounding color: %q", got)
	}
}

func TestPaletteSecret(t *testing.T) {
	if got := Colored.Secret("k"); got != codeBlue+"k"+codeReset {
		t.Errorf("got %q", got)
	}
}

func TestPaletteMarksPlain(t *testing.T) {
	var p Palette
	if p.Name("luar") != "luar" || p.Secret("k") != "k" {
		t.Errorf("plain palette adds codes: %q %q", p.Name("luar"), p.Secret("k"))
	}
}
