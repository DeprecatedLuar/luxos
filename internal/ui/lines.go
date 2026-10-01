package ui

import (
	"fmt"
	"strings"
)

// Line is one labelled line of an indented tree; Value, Dim and Children are
// optional.
type Line struct {
	Label    string
	Value    string
	Dim      string
	Children []Line
}

// View is a headed tree of lines: a marker and name, an optional note, then
// the lines beneath.
type View struct {
	Marker    string
	Color     Color
	Name      string
	Note      string
	NoteColor Color
	Lines     []Line
}

// RenderView writes v to w.
func RenderView(w *strings.Builder, v View, pal Palette) {
	fmt.Fprintf(w, "%s%s %s%s", pal.Tint(v.Color), v.Marker, v.Name, pal.reset)
	if v.Note != "" {
		fmt.Fprintf(w, " %s%s%s", pal.Tint(v.NoteColor), v.Note, pal.reset)
	}
	w.WriteString("\n")
	renderLines(w, v.Lines, "", pal)
}

func renderLines(w *strings.Builder, lines []Line, prefix string, pal Palette) {
	width := 0
	for _, l := range lines {
		if len(l.Label) > width {
			width = len(l.Label)
		}
	}
	for i, l := range lines {
		connector, childPrefix := branch, prefix+trunk
		if i == len(lines)-1 {
			connector, childPrefix = lastBranch, prefix+blank
		}
		fmt.Fprintf(w, "%s%s%s%s", pal.line, prefix, connector, pal.reset)
		if l.Label != "" {
			fmt.Fprintf(w, "%s%s%s", pal.title, l.Label, pal.reset)
			if l.Value != "" {
				w.WriteString(strings.Repeat(" ", width-len(l.Label)+2))
			}
		}
		w.WriteString(l.Value)
		if l.Dim != "" {
			fmt.Fprintf(w, "%s%s%s", pal.off, l.Dim, pal.reset)
		}
		w.WriteString("\n")
		renderLines(w, l.Children, childPrefix, pal)
	}
}
