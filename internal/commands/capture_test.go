package commands

import (
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
)

const wantMisuseCode = 2

var ansiCodes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// captureBoth runs fn with os.Stdout and os.Stderr redirected and returns what each received, without color codes.
func captureBoth(t *testing.T, fn func()) (stdout, stderr string) {
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
	return ansiCodes.ReplaceAllString(string(o), ""), ansiCodes.ReplaceAllString(string(e), "")
}

// assertMisuse checks that fn exits 2 with every want on stderr and nothing on stdout.
func assertMisuse(t *testing.T, fn func() error, want ...string) {
	t.Helper()
	var err error
	stdout, stderr := captureBoth(t, func() { err = fn() })
	var code shared.ExitCode
	if !errors.As(err, &code) || int(code) != wantMisuseCode {
		t.Fatalf("err = %v, want exit code %d", err, wantMisuseCode)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, w := range want {
		if !strings.Contains(stderr, w) {
			t.Errorf("stderr lacks %q:\n%s", w, stderr)
		}
	}
}

// assertHelp checks that fn succeeds with want on stdout and nothing on stderr.
func assertHelp(t *testing.T, fn func() error, want string) {
	t.Helper()
	var err error
	stdout, stderr := captureBoth(t, func() { err = fn() })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, want) || stderr != "" {
		t.Errorf("stdout = %q, stderr = %q, want stdout containing %q and empty stderr", stdout, stderr, want)
	}
}
