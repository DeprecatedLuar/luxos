package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	clearLine   = "\r\x1b[K"
	wrapOff     = "\x1b[?7l" // a status line wider than the terminal is clipped, not wrapped
	wrapOn      = "\x1b[?7h"
	donePrefix  = "✓ "
	warnPrefix  = "Warning: "
	errorPrefix = "Error: "
)

// Progress prints a command's progress to a writer and keeps the first write
// error, so a broken output stream is reported rather than dropped while the
// caller keeps sequencing. Started on a terminal, each Printf replaces the
// text of one spinner line; changes, warnings and errors always stay on screen.
type Progress struct {
	w    io.Writer
	pal  Palette
	term bool

	mu   sync.Mutex
	err  error
	text string
	stop chan struct{} // closed to end the spinner; nil while none runs
	done chan struct{} // closed once the spinner goroutine has returned
}

// NewProgress returns a Progress writing to w, colored and able to spin when
// w is a terminal.
func NewProgress(w io.Writer) *Progress {
	p := &Progress{w: w}
	if f, ok := w.(*os.File); ok {
		p.pal = PaletteFor(f)
		p.term = IsTerminal(f)
	}
	return p
}

// Printf sets the spinner line's text while the spinner runs, and writes a
// line otherwise.
func (p *Progress) Printf(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stop != nil {
		p.text = strings.TrimSpace(fmt.Sprintf(format, a...))
		return
	}
	p.write(fmt.Sprintf(format, a...))
}

// Changef prints a line that stays, reporting a change to the user's files.
func (p *Progress) Changef(format string, a ...any) {
	p.keep("", fmt.Sprintf(format, a...))
}

// Warnf prints "Warning: <message>" on its own line, yellow on a color terminal.
func (p *Progress) Warnf(format string, a ...any) {
	p.keep(p.pal.yellow, warnPrefix+fmt.Sprintf(format, a...))
}

// Errf prints "Error: <message>" on its own line, red on a color terminal.
func (p *Progress) Errf(format string, a ...any) {
	p.keep(p.pal.red, errorPrefix+fmt.Sprintf(format, a...))
}

// Start runs the spinner until Stop or Done. It does nothing unless the
// writer is a terminal.
func (p *Progress) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.term || p.stop != nil {
		return
	}
	p.stop, p.done = make(chan struct{}), make(chan struct{})
	go p.spin(p.stop, p.done)
}

// Stop ends the spinner and clears its line. It does nothing when no spinner
// runs.
func (p *Progress) Stop() {
	p.mu.Lock()
	stop, done := p.stop, p.done
	p.stop = nil
	p.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
	p.mu.Lock()
	defer p.mu.Unlock()
	p.write(clearLine)
}

// Done ends the spinner and leaves "✓ <label>" in its place. Off a terminal it
// prints nothing.
func (p *Progress) Done(label string) {
	if !p.term {
		return
	}
	p.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.write(p.pal.green + donePrefix + label + p.pal.reset + "\n")
}

// Err returns the first write error, or nil.
func (p *Progress) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Error writes "Error: <message>" to f, red on a color terminal.
func Error(f *os.File, err error) {
	pal := PaletteFor(f)
	fmt.Fprintln(f, pal.red+errorPrefix+err.Error()+pal.reset)
}

func (p *Progress) spin(stop, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(spinnerInterval)
	defer tick.Stop()
	for i := 0; ; i++ {
		p.mu.Lock()
		p.write(clearLine + wrapOff + spinnerLine(p.pal, i, p.text) + wrapOn)
		p.mu.Unlock()
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

// keep prints a line that stays, above the spinner line when one runs.
func (p *Progress) keep(color, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stop != nil {
		p.write(clearLine)
	}
	p.write(color + msg + p.pal.reset + "\n")
}

// write writes s unless an earlier write failed. The caller holds mu.
func (p *Progress) write(s string) {
	if p.err == nil {
		_, p.err = io.WriteString(p.w, s)
	}
}
