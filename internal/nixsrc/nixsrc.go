// Package nixsrc reads and edits Nix source files, one file at a time. Reads work on normalized
// nix-instantiate --parse output; edits work on raw source and always end in Write. It knows nothing
// about units, hosts or module trees.
//
// Every function takes an ABSOLUTE file path: nix-instantiate resolves a relative argument against
// the physical cwd, which walks through a symlinked CONFIG_DIR to the link target instead of the
// link. nix.Parse refuses a non-absolute path outright.
package nixsrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// tempPattern names the scratch file Write validates a candidate in.
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

	tmp, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if _, err := nix.Parse(tmpPath); err != nil {
		return fmt.Errorf("rewritten %s would fail to parse: %w", real, err)
	}

	f, err := os.OpenFile(real, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(content)
	return err
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}
