package ui

import (
	"fmt"
	"io"
	"os"
)

// PaletteOf is PaletteFor when w is a file, and the zero Palette otherwise.
func PaletteOf(w io.Writer) Palette {
	if f, ok := w.(*os.File); ok {
		return PaletteFor(f)
	}
	return Palette{}
}

// Done writes "✓ <msg>", green on a color terminal. Unlike Progress.Done it
// prints off a terminal too.
func Done(w io.Writer, msg string) {
	pal := PaletteOf(w)
	fmt.Fprintln(w, pal.green+donePrefix+msg+pal.reset)
}

// Warn writes "[WARNING] <msg>", yellow on a color terminal.
func Warn(w io.Writer, msg string) {
	pal := PaletteOf(w)
	fmt.Fprintln(w, pal.yellow+warnPrefix+msg+pal.reset)
}

// Error writes "[ERROR] <message>", red on a color terminal.
func Error(w io.Writer, err error) {
	pal := PaletteOf(w)
	fmt.Fprintln(w, pal.red+errorPrefix+err.Error()+pal.reset)
}

// Text writes msg on its own line.
func Text(w io.Writer, msg string) {
	fmt.Fprintln(w, msg)
}

// Marked writes "<glyph> <text>" in the tint of c: the state a thing is in,
// the same glyph and color a listing draws it with.
func Marked(w io.Writer, c Color, glyph, text string) {
	fmt.Fprintln(w, markedLine(PaletteOf(w), c, glyph, text))
}

func markedLine(pal Palette, c Color, glyph, text string) string {
	return pal.Tint(c) + glyph + " " + text + pal.reset
}

// Print writes s as is: a rendered view, JSON, the logo.
func Print(w io.Writer, s string) {
	io.WriteString(w, s)
}

// Name marks s as a name: underlined, leaving the surrounding color on.
func (p Palette) Name(s string) string {
	return p.underline + s + p.noUnderline
}

// Secret marks s as a secret to copy: blue.
func (p Palette) Secret(s string) string {
	return p.blue + s + p.reset
}
