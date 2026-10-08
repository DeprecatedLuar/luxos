// Package nix is the adapter for Nix: it runs the Nix tools through shell and
// reads and edits Nix source by asking Nix for its structure. It never touches
// the network itself.
//
// Every source function takes an ABSOLUTE file path: nix-instantiate resolves a relative argument
// against the physical cwd, which walks through a symlinked CONFIG_DIR to the link target instead of
// the link. Parse refuses a non-absolute path outright.
package nix

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const instantiateBin = "nix-instantiate"

const tempPattern = "luxos-nix-*.nix"

// Available reports whether nix-instantiate is on PATH.
func Available() bool {
	return shell.Has(instantiateBin)
}

// Parse runs `nix-instantiate --parse <absPath>` and returns its stdout.
// absPath must be an absolute path; the binary must be on PATH. On failure
// the returned error includes the command's stderr.
func Parse(absPath string) (string, error) {
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("nix.Parse: path %q is not absolute", absPath)
	}

	out, err := shell.Output(shell.Cmd{Bin: instantiateBin, Args: []string{"--parse", absPath}})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// EvalJSON runs `nix-instantiate --eval --strict --json --expr <expr>` with
// args passed through as --argstr, and returns its stdout. On failure the
// returned error includes the command's stderr.
func EvalJSON(expr string, args map[string]string) ([]byte, error) {
	argv := []string{"--eval", "--strict", "--json", "--expr", expr}
	for k, v := range args {
		argv = append(argv, "--argstr", k, v)
	}
	return shell.Output(shell.Cmd{Bin: instantiateBin, Args: argv})
}

// parse runs Parse and trims the trailing newline nix-instantiate always
// appends; every regex in this package (formalsRe's "$" in particular) is
// written against the trimmed output.
func parse(file string) (string, error) {
	out, err := Parse(file)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// Write replaces file's content, the one writer of hand-owned Nix files:
// resolve the symlink, validate content with nix.Parse on a temp file, then
// open the real path O_WRONLY|O_TRUNC and write in place (never rename), so
// ownership and mode survive running as root. A candidate that does not
// parse leaves file untouched.
func Write(file string, content []byte) error {
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		return err
	}

	err = WithTemp(tempPattern, content, func(path string) error {
		if _, err := Parse(path); err != nil {
			return fmt.Errorf("rewritten %s would fail to parse: %w", real, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	f, err := os.OpenFile(real, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, err = f.Write(content)
	return errors.Join(err, f.Close())
}

// WithTemp writes content to a scratch file named by pattern, calls fn with
// its path and removes the file, returning every error that occurred.
func WithTemp(pattern string, content []byte, fn func(path string) error) (err error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.Remove(tmp.Name())) }()

	_, err = tmp.Write(content)
	if err = errors.Join(err, tmp.Close()); err != nil {
		return err
	}
	return fn(tmp.Name())
}

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}
