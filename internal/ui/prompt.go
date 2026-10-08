package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Ask prints prompt to w and reads one line from stdin. End of input reads as
// an empty line.
func Ask(w io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(w, prompt); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// IsYes reports whether reply is an affirmative answer.
func IsYes(reply string) bool {
	return reply == "y" || reply == "Y"
}

// Confirm asks prompt on stdout. An empty line or end of input returns defYes.
func Confirm(prompt string, defYes bool) (bool, error) {
	reply, err := Ask(os.Stdout, prompt)
	if err != nil {
		return false, err
	}
	if reply == "" {
		return defYes, nil
	}
	return IsYes(reply), nil
}

// Prompter asks questions over one buffered reader, so lines typed ahead are
// not lost between prompts.
type Prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{in: bufio.NewReader(in), out: out}
}

// Ask prints prompt and returns the next line, trimmed. Input that ends with
// nothing typed returns io.EOF.
func (p *Prompter) Ask(prompt string) (string, error) {
	if _, err := fmt.Fprint(p.out, prompt); err != nil {
		return "", err
	}
	line, err := p.in.ReadString('\n')
	if err == io.EOF && line == "" {
		return "", io.EOF
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
