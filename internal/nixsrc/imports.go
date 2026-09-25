package nixsrc

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	// Recognized import line (implementation-plan.md §3): optional leading
	// whitespace, "./" + a path with no whitespace or "#", optional
	// whitespace, optional trailing "#..." comment. A line starting with
	// "#" is a plain comment and never matches.
	lineRe = regexp.MustCompile(`^[ \t]*\./([^ \t#]+)[ \t]*(#.*)?$`)

	// The block's opening line: "imports = [" alone, or the inline-empty
	// "imports = [];" form. Anything else after the "[" (e.g. items on the
	// same line) is a shape this package doesn't understand.
	blockStartRe = regexp.MustCompile(`^[ \t]*imports[ \t]*=[ \t]*\[[ \t]*(\];)?[ \t]*$`)

	// Any line that merely starts an "imports =" assignment — used to
	// detect a shape blockStartRe doesn't recognize (an "imports =" line
	// exists but isn't one of the two forms above).
	blockLooseStartRe = regexp.MustCompile(`^[ \t]*imports[ \t]*=[ \t]*\[`)

	// The block's closing line, multi-line form only (the inline-empty
	// form has no separate closing line).
	blockEndRe = regexp.MustCompile(`^[ \t]*];[ \t]*$`)

	// Trailing inline-empty marker on the opening line itself.
	inlineEndRe = regexp.MustCompile(`\];[ \t]*$`)

	// Leading whitespace of a line, and of a "./..." item line.
	leadingWSRe = regexp.MustCompile(`^[ \t]*`)
)

// item is one recognized import line found inside a file's single block.
type item struct {
	lineNo  int // 1-indexed
	path    string
	comment string // "" or "#..."
	leading string
}

// ListImports returns the import paths in file's single imports block, bare
// (no "./"), in file order. ok is false when file has no single recognizable block.
func ListImports(file string) (paths []string, ok bool, err error) {
	items, ok, err := itemLines(file)
	if err != nil || !ok {
		return nil, ok, err
	}
	paths = make([]string, len(items))
	for i, it := range items {
		paths[i] = it.path
	}
	return paths, true, nil
}

// AddImport appends one import line for path (leading "./" optional) to file's
// single imports block. Converts the inline-empty "imports = [];" form to
// multi-line as needed. A no-op if path is already present. Everything
// else in the file is left byte-for-byte.
func AddImport(file, path string) error {
	path = strings.TrimPrefix(path, "./")

	lines, err := readLines(file)
	if err != nil {
		return err
	}

	s, e, inline, ok := findBlock(lines)
	if !ok {
		return shapeError(file)
	}

	items, err := blockItems(lines, s, e, inline)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.path == path {
			return nil
		}
	}

	leading := leadingWSRe.FindString(lines[s-1])
	itemIndent := leading + "  "

	var newLines []string
	if inline {
		for idx, l := range lines {
			if idx+1 == s {
				newLines = append(newLines, leading+"imports = [")
				newLines = append(newLines, itemIndent+"./"+path)
				newLines = append(newLines, leading+"];")
			} else {
				newLines = append(newLines, l)
			}
		}
	} else {
		if e-s > 1 {
			if existing := leadingWSRe.FindString(lines[s]); existing != "" {
				itemIndent = existing
			}
		}
		for idx, l := range lines {
			if idx+1 == e {
				newLines = append(newLines, itemIndent+"./"+path)
			}
			newLines = append(newLines, l)
		}
	}

	return writeLines(file, newLines)
}

// RemoveImport deletes every import line in file's single block whose path
// equals path (bare, "./" optional). A no-op (no write) if none match.
func RemoveImport(file, path string) error {
	path = strings.TrimPrefix(path, "./")

	lines, err := readLines(file)
	if err != nil {
		return err
	}

	s, e, inline, ok := findBlock(lines)
	if !ok {
		return shapeError(file)
	}
	if inline {
		return nil
	}

	var newLines []string
	removed := false
	for idx, l := range lines {
		ln := idx + 1
		if ln > s && ln < e {
			if m := lineRe.FindStringSubmatch(l); m != nil && m[1] == path {
				removed = true
				continue
			}
		}
		newLines = append(newLines, l)
	}

	if !removed {
		return nil
	}
	return writeLines(file, newLines)
}

// RetargetImports rewrites every import line whose path satisfies match to
// ./newPath (keeping its indent and trailing comment), or deletes it when
// newPath == "". A line already at newPath is untouched. Returns the old paths
// changed. ok is false when file has no single recognizable block (nothing written).
func RetargetImports(file string, match func(path string) bool, newPath string) (old []string, ok bool, err error) {
	newPath = strings.TrimPrefix(newPath, "./")

	items, ok, err := itemLines(file)
	if err != nil || !ok {
		return nil, ok, err
	}
	if len(items) == 0 {
		return nil, true, nil
	}

	lines, err := readLines(file)
	if err != nil {
		return nil, false, err
	}

	toDelete := make(map[int]bool)
	for _, it := range items {
		if !match(it.path) {
			continue
		}
		if newPath != "" {
			if it.path == newPath {
				continue
			}
			newLine := it.leading + "./" + newPath
			if it.comment != "" {
				newLine = newLine + " " + it.comment
			}
			lines[it.lineNo-1] = newLine
		} else {
			toDelete[it.lineNo] = true
		}
		old = append(old, it.path)
	}

	if len(old) == 0 {
		return nil, true, nil
	}

	if len(toDelete) > 0 {
		var kept []string
		for idx, l := range lines {
			if toDelete[idx+1] {
				continue
			}
			kept = append(kept, l)
		}
		lines = kept
	}

	if err := writeLines(file, lines); err != nil {
		return nil, false, err
	}
	return old, true, nil
}

// readLines reads file's content split into lines without trailing
// newlines.
func readLines(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	content := string(data)
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil, nil
	}
	return strings.Split(content, "\n"), nil
}

// findBlock finds the single recognizable imports block in lines. Returns
// 1-indexed start/end (inclusive), whether it's the inline-empty
// single-line form (start == end, no items), and whether exactly one
// recognizable block was found.
func findBlock(lines []string) (start, end int, inline bool, ok bool) {
	strictCount, looseCount := 0, 0
	strictIdx := -1
	for i, l := range lines {
		if blockStartRe.MatchString(l) {
			strictCount++
			if strictIdx == -1 {
				strictIdx = i
			}
		}
		if blockLooseStartRe.MatchString(l) {
			looseCount++
		}
	}
	if strictCount != looseCount || strictCount != 1 {
		return 0, 0, false, false
	}

	startContent := lines[strictIdx]
	if inlineEndRe.MatchString(startContent) {
		return strictIdx + 1, strictIdx + 1, true, true
	}

	for i := strictIdx + 1; i < len(lines); i++ {
		if blockEndRe.MatchString(lines[i]) {
			return strictIdx + 1, i + 1, false, true
		}
	}

	return 0, 0, false, false
}

// blockItems extracts the recognized import lines between an already-found
// block's boundaries (see findBlock).
func blockItems(lines []string, s, e int, inline bool) ([]item, error) {
	if inline {
		return nil, nil
	}
	var items []item
	for i := s; i < e-1; i++ {
		ln := i + 1
		l := lines[i]
		m := lineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		items = append(items, item{
			lineNo:  ln,
			path:    m[1],
			comment: m[2],
			leading: leadingWSRe.FindString(l),
		})
	}
	return items, nil
}

// itemLines finds file's single block and returns its recognized import
// lines. ok is false when the file isn't recognizable (more/less than one
// block, or an unrecognized shape) — the tolerant counterpart to the
// writers' shapeError, for callers that loop every host and must not abort
// on one bad file.
func itemLines(file string) ([]item, bool, error) {
	lines, err := readLines(file)
	if err != nil {
		return nil, false, err
	}
	s, e, inline, ok := findBlock(lines)
	if !ok {
		return nil, false, nil
	}
	items, err := blockItems(lines, s, e, inline)
	if err != nil {
		return nil, false, err
	}
	return items, true, nil
}

// shapeError is the standard "shape not recognized" error for file.
func shapeError(file string) error {
	return fmt.Errorf("%s does not have exactly one recognizable 'imports = [ ... ];' block", file)
}

// writeLines joins lines into file's new full content and hands it to Write.
func writeLines(file string, lines []string) error {
	return Write(file, []byte(strings.Join(lines, "\n")+"\n"))
}
