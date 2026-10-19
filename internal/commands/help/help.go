package help

import (
	_ "embed"
	"io"
	"os"

	gohelp "github.com/DeprecatedLuar/gohelp-luar"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	misuseExitCode = 2
	usageLine      = "usage: luxos <command> [args]"
	hintLine       = "run 'luxos help' for commands"
	topicArgCount  = 2
)

//go:embed help.md
var spec string

// Run prints full help or a topic to stdout; args is the argv tail starting at "help".
func Run(args []string) error {
	err := gohelp.RunSpec(args, spec)
	if err == nil || len(args) != topicArgCount {
		return err
	}
	if Command(os.Stdout, args[1]) != nil {
		return err
	}
	return nil
}

// Command writes the root page's block for name to w.
func Command(w io.Writer, name string) error {
	parsed, err := gohelp.Parse(spec)
	if err != nil {
		return err
	}
	return parsed.Root.FprintSection(w, name)
}

// Misuse writes cause (when non-nil) via ui.Error, then name's block, to w; returns shared.ExitCode(2).
func Misuse(w io.Writer, name string, cause error) error {
	if cause != nil {
		ui.Error(w, cause)
	}
	if err := Command(w, name); err != nil {
		return err
	}
	return shared.ExitCode(misuseExitCode)
}

// Brief writes cause (when non-nil) via ui.Error, then the two root usage lines, to w; returns shared.ExitCode(2).
func Brief(w io.Writer, cause error) error {
	if cause != nil {
		ui.Error(w, cause)
	}
	ui.Text(w, usageLine)
	ui.Text(w, hintLine)
	return shared.ExitCode(misuseExitCode)
}
