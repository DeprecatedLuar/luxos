package ui

import (
	"fmt"
	"io"
)

// Progress prints to a writer and keeps the first write error, so a broken
// output stream is reported rather than dropped while the caller keeps
// sequencing.
type Progress struct {
	w   io.Writer
	err error
}

// NewProgress returns a Progress writing to w.
func NewProgress(w io.Writer) *Progress { return &Progress{w: w} }

// Printf writes to the writer unless an earlier write failed.
func (p *Progress) Printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// Err returns the first write error, or nil.
func (p *Progress) Err() error { return p.err }
