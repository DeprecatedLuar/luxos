// Package shell starts external programs. It knows how to run a program, not
// what any program is for.
package shell

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Cmd is one program invocation. An empty Dir runs in the current directory;
// a nil Env inherits this process's environment.
type Cmd struct {
	Bin  string
	Args []string
	Dir  string
	Env  []string
}

func (c Cmd) String() string {
	return strings.Join(append([]string{c.Bin}, c.Args...), " ")
}

func (c Cmd) command() *exec.Cmd {
	cmd := exec.Command(c.Bin, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	return cmd
}

// Run runs c on the terminal's stdin, stdout and stderr.
func Run(c Cmd) error {
	cmd := c.command()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Output returns c's stdout. On failure the error carries c's stderr.
func Output(c Cmd) ([]byte, error) {
	cmd := c.command()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if _, exited := ExitCode(err); exited {
		return nil, fmt.Errorf("%s failed:\n%s", c, stderr.String())
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c, err)
	}
	return out, nil
}

// OutputLive returns c's stdout while its stderr goes to the terminal.
func OutputLive(c Cmd) ([]byte, error) {
	cmd := c.command()
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c, err)
	}
	return out, nil
}

// Has reports whether bin is on PATH.
func Has(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

// Exec replaces this process with bin, looked up on PATH.
func Exec(bin string, args []string) error {
	path, err := exec.LookPath(bin)
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{path}, args...), os.Environ())
}

// ExitCode reports the exit status a finished program left in err.
func ExitCode(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// Self returns the path of this binary with symlinks resolved.
func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
