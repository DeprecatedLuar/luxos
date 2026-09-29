package gitignore

import (
	"bufio"
	"errors"
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/userfile"
)

// Exact match, ignoring a trailing '\r' and trailing whitespace. Preserves
// existing content and order. Creates the file if it does not exist; a created
// file gets its parent directory's owner. Returns the lines that were actually added, in the order given.
func Ensure(path string, lines []string) (added []string, err error) {
	existing, err := readLines(path)
	if err != nil {
		return nil, err
	}

	present := make(map[string]bool, len(existing))
	for _, l := range existing {
		present[normalize(l)] = true
	}

	var toAdd []string
	for _, l := range lines {
		key := normalize(l)
		if present[key] {
			continue
		}
		present[key] = true
		toAdd = append(toAdd, l)
	}

	if len(toAdd) == 0 {
		return nil, nil
	}

	_, statErr := os.Lstat(path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	missing := statErr != nil

	var b strings.Builder
	if !missing && needsLeadingNewline(path) {
		b.WriteString("\n")
	}
	for _, l := range toAdd {
		b.WriteString(l)
		b.WriteString("\n")
	}

	if missing {
		if err := userfile.Write(path, []byte(b.String())); err != nil {
			return nil, err
		}
		return toAdd, nil
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, err
	}
	_, err = f.WriteString(b.String())
	if err := errors.Join(err, f.Close()); err != nil {
		return nil, err
	}

	return toAdd, nil
}

func readLines(path string) (lines []string, err error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func needsLeadingNewline(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return false
	}
	return data[len(data)-1] != '\n'
}

func normalize(line string) string {
	line = strings.TrimRight(line, "\r")
	line = strings.TrimRight(line, " \t")
	return line
}
