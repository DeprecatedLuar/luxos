package main

import (
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
)

const (
	briefLines   = "usage: luxos <command> [args]\nrun 'luxos help' for commands\n"
	wantExitCode = 2
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// capture runs fn with os.Stdout and os.Stderr redirected and returns what each received.
func capture(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()
	fn()
	outW.Close()
	errW.Close()
	o, _ := io.ReadAll(outR)
	e, _ := io.ReadAll(errR)
	return ansi.ReplaceAllString(string(o), ""), ansi.ReplaceAllString(string(e), "")
}

func assertExit2(t *testing.T, err error) {
	t.Helper()
	var code shared.ExitCode
	if !errors.As(err, &code) || int(code) != wantExitCode {
		t.Fatalf("err = %v, want exit code %d", err, wantExitCode)
	}
}

func TestStripDeadlock(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		rest  []string
		found bool
	}{
		{"absent", []string{"rb", "switch"}, []string{"rb", "switch"}, false},
		{"last", []string{"passwd", "ana", "--deadlock"}, []string{"passwd", "ana"}, true},
		{"first", []string{"--deadlock", "rb", "switch"}, []string{"rb", "switch"}, true},
		{"middle", []string{"rb", "--deadlock", "switch"}, []string{"rb", "switch"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, found := stripDeadlock(tt.args)
			if found != tt.found || !slices.Equal(rest, tt.rest) {
				t.Errorf("stripDeadlock(%q) = %q, %v; want %q, %v", tt.args, rest, found, tt.rest, tt.found)
			}
		})
	}
}

func TestRunNoArgs(t *testing.T) {
	var err error
	stdout, stderr := capture(t, func() { err = run(nil) })
	assertExit2(t, err)
	if stdout != "" || stderr != briefLines {
		t.Errorf("stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var err error
	stdout, stderr := capture(t, func() { err = run([]string{"frobnicate"}) })
	assertExit2(t, err)
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `unknown command "frobnicate"`) || !strings.HasSuffix(stderr, briefLines) {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestRunHelp(t *testing.T) {
	var err error
	stdout, _ := capture(t, func() { err = run([]string{"--help"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "──[module]") {
		t.Errorf("stdout lacks module block: %q", stdout)
	}
}
