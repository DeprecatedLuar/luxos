// Package ui renders luxos output for a terminal and asks the user questions.
// It knows nothing about modules, flakes or hosts: callers describe what to
// draw as rows and lines.
package ui

import (
	"os"
)

const noColorEnv = "NO_COLOR"

// Tree palette (terminal only, disabled by NO_COLOR): drawn from Moonlight-inspired
// swatches. Markers carry the state colors; the tree adds three neutrals,
// ordered darkest to lightest: connectors, then category names, then the off
// state, never white, so the off marker stays visibly the quietest color on
// screen while remaining legible.
const (
	codeGreen     = "\x1b[38;2;204;243;145m"        // #CCF391
	codeTeal      = "\x1b[1m\x1b[38;2;125;250;206m" // #7DFACE bold
	codeRed       = "\x1b[38;2;237;112;122m"        // #ED707A
	codePurple    = "\x1b[38;2;181;166;250m"        // #B5A6FA
	codeBlue      = "\x1b[38;2;130;170;255m"        // #82AAFF
	codeNix       = "\x1b[38;2;126;186;228m"        // #7EBAE4 NixOS blue: flake marks
	codeYellow    = "\x1b[38;2;255;199;119m"        // #FFC777
	codeLine      = "\x1b[38;2;74;79;115m"          // #212436 lightened: tree connectors
	codeTitle     = "\x1b[1m\x1b[38;2;107;112;137m" // #6B7089 bold: category names
	codeOff       = "\x1b[38;2;156;163;196m"        // #9CA3C4
	codeReset     = "\x1b[0m"
	codeUnderline = "\x1b[4m"
	codeStrike    = "\x1b[9m"
	codeItalic    = "\x1b[3m"
)

// Color is a tint a row or note is drawn in. The zero value is the quiet one.
type Color int

const (
	ColorOff Color = iota
	ColorGreen
	ColorTeal
	ColorRed
	ColorPurple
	ColorBlue
	ColorNix
	ColorYellow
	ColorLine
)

// Palette is the set of color codes rendering tints with; the zero Palette
// renders the same shapes with no escape codes.
type Palette struct {
	green, teal, red, purple, blue, nix, yellow, line, title, off, underline, strike, reset string
}

// Colored is the palette of a color terminal.
var Colored = Palette{
	green: codeGreen, teal: codeTeal, red: codeRed, purple: codePurple, blue: codeBlue, nix: codeNix, yellow: codeYellow,
	line: codeLine, title: codeTitle, off: codeOff, underline: codeUnderline, strike: codeStrike, reset: codeReset,
}

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// PaletteFor is Colored when f is a terminal and NO_COLOR is unset, and the
// zero Palette otherwise.
func PaletteFor(f *os.File) Palette {
	if IsTerminal(f) && os.Getenv(noColorEnv) == "" {
		return Colored
	}
	return Palette{}
}

// Tint returns the escape code of c.
func (p Palette) Tint(c Color) string {
	switch c {
	case ColorTeal:
		return p.teal
	case ColorGreen:
		return p.green
	case ColorRed:
		return p.red
	case ColorPurple:
		return p.purple
	case ColorBlue:
		return p.blue
	case ColorNix:
		return p.nix
	case ColorYellow:
		return p.yellow
	case ColorLine:
		return p.line
	default:
		return p.off
	}
}

// Italic returns s in italics when f is a color terminal, and s otherwise.
func Italic(f *os.File, s string) string {
	if PaletteFor(f) == (Palette{}) {
		return s
	}
	return codeItalic + s + codeReset
}
