package ui

import "time"

// The look of a started Progress: its frames, their pace and how one line is
// drawn. Everything else about the spinner is mechanics in progress.go.
const spinnerInterval = 80 * time.Millisecond

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerLine is the status line drawn on tick i: the frame, then text.
func spinnerLine(pal Palette, i int, text string) string {
	frame := spinnerFrames[i%len(spinnerFrames)]
	return pal.blue + frame + pal.reset + " " + pal.off + text + pal.reset
}
