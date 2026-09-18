// Package imports is the sole reader/writer of host entrypoints
// (implementation-plan.md #18): .local/<host>/modules.nix files, the
// host's selection (L4). It reads and writes lines inside the single
// recognized
// "imports = [ ... ];" block — hand-written and tooling-written lines
// alike, indistinguishably. Everything else in the file (let, options,
// comments, a commented-out import) is left byte-for-byte (#7). This is
// not a Nix parser: a file with more than one recognizable block, or one
// written some other way (computed imports, a single-line block with
// items), is refused by the writers (Add/Remove) and its lines are
// skipped, with a warning, by the read paths that loop every host
// (Importers/Retarget/Heal).
//
// Path convention used throughout this file's public API: a "path" is
// relative to CONFIG_DIR/modules with no leading "./" (the same shape
// units.Resolve returns) — List strips it on read, Add/Retarget add it
// back on write.
package imports

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

// entrypointName is the file name every host's modules selection lives in,
// found under <LOCAL_DIR>/*/modules.nix (L4).
const entrypointName = "modules.nix"

// localPrefix marks an import path as belonging to the active host's own
// local modules (L5/L6), e.g. "local/foo.nix".
const localPrefix = "local/"

// localModulesSubdir is the directory name under <LOCAL_DIR>/<host>/ that
// holds that host's local modules (L5).
const localModulesSubdir = "modules"

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

// Change is one rewrite made by Retarget or Heal. New == "" means the line
// was removed.
type Change struct {
	File, Old, New string
}

// item is one recognized import line found inside a file's single block.
type item struct {
	lineNo  int // 1-indexed
	path    string
	comment string // "" or "#..."
	leading string
}

// NameFromPath is the public wrapper for §3 "Name from an import path",
// re-exported here since other callers (e.g. `lux module`'s state markers)
// need the same logic imports itself uses.
func NameFromPath(path string) string {
	return units.NameFromPath(path)
}

// List returns the active import paths in file, bare (no leading "./"),
// in file order. Hard error if file doesn't have exactly one recognizable
// imports block.
func List(file string) ([]string, error) {
	items, ok, err := itemLines(file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, shapeError(file)
	}

	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.path
	}
	return paths, nil
}

// Add appends one import line for path (leading "./" optional) to file's
// single imports block. Converts the inline-empty "imports = [];" form to
// multi-line as needed. A no-op if path is already present. Everything
// else in the file is left byte-for-byte.
func Add(file, path string) error {
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

// Remove deletes every import line in file's single block whose path
// equals path (bare, "./" optional). A no-op (no write) if none match.
func Remove(file, path string) error {
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

// Importers returns every entrypoint file with a line whose name (from its
// path, per NameFromPath) matches name — stale paths included. Read-only.
// host == "" scans every <localDir>/*/modules.nix; otherwise only
// <localDir>/<host>/modules.nix is considered. A host whose file isn't
// recognizable is silently skipped (same tolerance as Retarget/Heal — it
// just can't be a hit).
func Importers(localDir, name, host string) ([]string, error) {
	files, err := entrypointsFor(localDir, host)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, file := range files {
		items, ok, err := itemLines(file)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, it := range items {
			if NameFromPath(it.path) == name {
				out = append(out, file)
				break
			}
		}
	}
	return out, nil
}

// Retarget is the writer of host entrypoints (#18): rewrite every import
// line whose name matches name to newPath, or delete it when newPath is
// "". host == "" reaches every <localDir>/*/modules.nix (the cross-host
// case, for a shared unit); otherwise only <localDir>/<host>/modules.nix is
// touched (the local-unit case, L7). Returns every change made; a no-op
// line (already at newPath) is left untouched and unreported, so a second
// run returns no changes. A host whose file isn't recognizable is warned
// about and left untouched.
func Retarget(localDir, name, newPath, host string) ([]Change, error) {
	changes, _, err := retarget(localDir, name, newPath, host)
	return changes, err
}

func retarget(localDir, name, newPath, host string) ([]Change, []string, error) {
	newPath = strings.TrimPrefix(newPath, "./")

	files, err := entrypointsFor(localDir, host)
	if err != nil {
		return nil, nil, err
	}

	var changes []Change
	var warnings []string

	for _, file := range files {
		items, ok, err := itemLines(file)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s does not have exactly one recognizable imports block, skipping", file))
			continue
		}
		if len(items) == 0 {
			continue
		}

		lines, err := readLines(file)
		if err != nil {
			return nil, nil, err
		}

		toDelete := make(map[int]bool)
		changed := false

		for _, it := range items {
			if NameFromPath(it.path) != name {
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
				changed = true
				changes = append(changes, Change{File: file, Old: it.path, New: newPath})
			} else {
				toDelete[it.lineNo] = true
				changed = true
				changes = append(changes, Change{File: file, Old: it.path, New: ""})
			}
		}

		if !changed {
			continue
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
			return nil, nil, err
		}
	}

	return changes, warnings, nil
}

// Heal rewrites every broken import line in every host's entrypoint,
// per host (L7): each host's selection lines are checked and resolved
// against that host's own unit set (shared units plus that host's own
// local/ units, per units.Walk(modulesDir, <localDir>/<host>/modules)).
//   - path exists (local/ prefixed against that host's local modules dir,
//     otherwise against modulesDir): keep.
//   - name resolves to a local/ unit: retarget scoped to this host only.
//   - name resolves to a shared unit: retarget in every host referencing
//     it (once per name, as before).
//   - name resolves to nothing, active host: error (collecting every such
//     line into one), or with prune: remove and report as a Change.
//   - name resolves to nothing, other host: warning, untouched.
//
// A host whose own units.Walk fails errors the whole call when it's
// activeHost, otherwise adds a warning ("<host>: <err>") and skips that
// host entirely. Heal is idempotent.
func Heal(localDir, modulesDir, activeHost string, prune bool) ([]Change, []string, error) {
	files, err := hostEntrypoints(localDir)
	if err != nil {
		return nil, nil, err
	}

	var changes []Change
	var warnings []string
	var unresolved []string // "file: ./path" for active-host unresolved names

	// Track which (host, name) local retargets and which shared-name
	// retargets have already been applied, so a name referenced by more
	// than one broken line in the same run is only retargeted once —
	// Retarget itself loops every relevant host already.
	handledLocal := make(map[string]bool)
	handledShared := make(map[string]bool)

	for _, file := range files {
		host := filepath.Base(filepath.Dir(file))

		us, err := units.Walk(modulesDir, filepath.Join(localDir, host, localModulesSubdir))
		if err != nil {
			if host == activeHost {
				return nil, nil, err
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s", host, err))
			continue
		}

		items, ok, err := itemLines(file)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s does not have exactly one recognizable imports block, skipping heal", file))
			continue
		}

		isActive := host == activeHost

		for _, it := range items {
			var fullPath string
			if rest, cut := strings.CutPrefix(it.path, localPrefix); cut {
				fullPath = filepath.Join(localDir, host, localModulesSubdir, rest)
			} else {
				fullPath = filepath.Join(modulesDir, it.path)
			}
			if _, err := os.Stat(fullPath); err == nil {
				continue
			}

			name := NameFromPath(it.path)
			if resolved, ok := units.Resolve(us, name); ok {
				if strings.HasPrefix(resolved, localPrefix) {
					key := host + "\x00" + name
					if handledLocal[key] {
						continue
					}
					handledLocal[key] = true
					rc, rw, err := retarget(localDir, name, resolved, host)
					if err != nil {
						return nil, nil, err
					}
					changes = append(changes, rc...)
					warnings = append(warnings, rw...)
				} else {
					if handledShared[name] {
						continue
					}
					handledShared[name] = true
					rc, rw, err := retarget(localDir, name, resolved, "")
					if err != nil {
						return nil, nil, err
					}
					changes = append(changes, rc...)
					warnings = append(warnings, rw...)
				}
				continue
			}

			if isActive {
				if prune {
					if err := Remove(file, it.path); err != nil {
						return nil, nil, err
					}
					changes = append(changes, Change{File: file, Old: it.path, New: ""})
				} else {
					unresolved = append(unresolved, fmt.Sprintf("%s: ./%s does not exist and '%s' does not resolve to any module", file, it.path, name))
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("%s: ./%s does not exist and '%s' does not resolve to any module — left untouched", file, it.path, name))
			}
		}
	}

	if len(unresolved) > 0 {
		msg := strings.Join(unresolved, "\n") + "\n  Fix the import by hand, or rerun with --prune to remove it."
		return changes, warnings, fmt.Errorf("%s", msg)
	}

	return changes, warnings, nil
}

//──[private helpers]─────────────────────────────────────────────────────

// hostEntrypoints returns every <localDir>/*/modules.nix that exists,
// sorted for deterministic iteration.
func hostEntrypoints(localDir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(localDir, "*", entrypointName))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			files = append(files, m)
		}
	}
	sort.Strings(files)
	return files, nil
}

// entrypointsFor returns the entrypoint file(s) a host-scoped call should
// act on: every <localDir>/*/modules.nix when host is "", or just
// <localDir>/<host>/modules.nix (if it exists) otherwise.
func entrypointsFor(localDir, host string) ([]string, error) {
	if host == "" {
		return hostEntrypoints(localDir)
	}
	file := filepath.Join(localDir, host, entrypointName)
	if fi, err := os.Stat(file); err != nil || fi.IsDir() {
		if err != nil && os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	return []string{file}, nil
}

// realPath resolves path to the real file behind any symlink.
func realPath(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return real, nil
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

// writeLines validates lines as file's new full content with nix.Parse,
// then replaces file's real target in place per §3 Write discipline:
// resolve the symlink, validate via a temp file, then
// O_WRONLY|O_TRUNC (never rename), so ownership and mode survive running
// as root.
func writeLines(file string, lines []string) error {
	real, err := realPath(file)
	if err != nil {
		return err
	}

	content := strings.Join(lines, "\n") + "\n"

	tmp, err := os.CreateTemp("", "luxos-imports-*.nix")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.WriteString(content); err != nil {
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

	if _, err := f.WriteString(content); err != nil {
		return err
	}
	return nil
}
