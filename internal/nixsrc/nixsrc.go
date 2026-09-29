// Package nixsrc reads and edits Nix source files, one file at a time. Reads work on normalized
// nix-instantiate --parse output; edits work on raw source and always end in Write. It knows nothing
// about units, hosts or module trees.
//
// Every function takes an ABSOLUTE file path: nix-instantiate resolves a relative argument against
// the physical cwd, which walks through a symlinked CONFIG_DIR to the link target instead of the
// link. nix.Parse refuses a non-absolute path outright.
package nixsrc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

const tempPattern = "luxos-nixsrc-*.nix"

// parse runs nix.Parse and trims the trailing newline nix-instantiate
// always appends — matching bash's command substitution, which strips it
// implicitly and is what every regex in this package (formalsRe's "$" in
// particular) is written against.
func parse(file string) (string, error) {
	out, err := nix.Parse(file)
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
		if _, err := nix.Parse(path); err != nil {
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
