// Package refs is the single owner of what a module file under
// CONFIG_DIR/modules may reference (implementation-plan.md #25, #26, #27,
// #28, §3 "Module boundary" and "luxos.modules") — paths, and
// luxos.modules names — as internal/imports is for host entrypoints (#18).
// Everything goes through nix.Parse; no hand-written Nix parsing.
//
// Path convention: every public function here takes an ABSOLUTE file path.
// nix-instantiate --parse resolves relative paths inside a file lexically
// against the path it was given, but a relative *argument* resolves against
// the physical cwd — walking through a symlinked CONFIG_DIR (the common
// case: ~/.config/luxos) would then silently resolve against the link
// target instead of the link. nix.Parse refuses a non-absolute path
// outright; callers of Validate get this for free since it always walks
// from an absolute modulesDir.
package refs

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

// entrypointName is the host-selection mirror file, exempt from Dependents
// and Retarget (it isn't a module file).
const entrypointName = "default.nix"

// modulesDirName is the fixed name Validate's "does not resolve" message
// refers to (matches bash's CONFIGGEN_MODULES_DIR constant, "modules").
const modulesDirName = "modules"

// Violation is one boundary, dynamic-path, call-shape or unresolved-name
// problem found by Validate. File is relative to modulesDir.
type Violation struct {
	File, Message string
}

// Change is one rewrite made by Retarget. New == "" means the element was
// removed rather than renamed.
type Change struct {
	File, Old, New string
}

var (
	// A path token as nix-instantiate --parse prints it: starts with "/"
	// followed by a non-"/" char (so the "//" update operator never
	// matches; division prints as __div and <nixpkgs> as __findFile, so
	// neither yields a path either).
	pathRe = regexp.MustCompile(`/[A-Za-z0-9._+-][A-Za-z0-9._+/-]*`)

	// The static base of a concatenation ("(<path> + <expr>)"), including
	// the bare root "/" base that pathRe deliberately excludes.
	dynamicRe = regexp.MustCompile(`(^|[ ([])/([A-Za-z0-9._+-][A-Za-z0-9._+/-]*)? \+ `)

	// Strips the outer lambda's formals declaration ("{ a, b, luxos,
	// ... }:") from the start of a parse — the one place "luxos" is
	// legitimately a bare identifier with no call attached.
	formalsRe = regexp.MustCompile(`^(\(+)(\{[^{}]*\}:)(.*)$`)

	// One recognized luxos.modules call as nix-instantiate --parse always
	// prints it, regardless of source formatting: "(luxos).modules [
	// ("a") ("b") ]" — a literal list of string-literal elements.
	callSpanRe = regexp.MustCompile(`\(luxos\)\.modules \[([^\[\]]*)\]`)

	// A call's inner text is valid only if it's a sequence of
	// ("literal") items and whitespace.
	validItemsRe = regexp.MustCompile(`^(\s*\("[^"]*"\))*\s*$`)

	// One ("literal") item inside a call's parsed inner text.
	parseItemRe = regexp.MustCompile(`\("([^"]*)"\)`)

	// A "luxos.modules [ ... ]" call as it looks in hand-written SOURCE
	// (any whitespace after "modules", no parens around "luxos").
	sourceSpanRe = regexp.MustCompile(`luxos\.modules\s*\[([^\[\]]*)\]`)

	// One "literal" item inside a source call's inner text.
	sourceItemRe = regexp.MustCompile(`"([^"]*)"`)
)

// parse runs nix.Parse and trims the trailing newline nix-instantiate
// always appends — matching bash's command substitution, which strips it
// implicitly and is what every regex above (formalsRe's "$" in particular)
// is written against.
func parse(file string) (string, error) {
	out, err := nix.Parse(file)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

//──[Public API]──────────────────────────────────────────────────────────────

// Owner returns the owning folder-module directory of file (absolute path,
// may be a file or a directory), or "" when none exists — the nearest
// ancestor strictly below modulesDir that has its own default.nix.
// Comparison is purely lexical (string prefix, per §3): never resolved with
// EvalSymlinks.
func Owner(modulesDir, file string) string {
	root := strings.TrimSuffix(modulesDir, "/")
	d := filepath.Dir(file)
	for strings.HasPrefix(d, root+"/") {
		if fi, err := os.Stat(filepath.Join(d, entrypointName)); err == nil && !fi.IsDir() {
			return d
		}
		d = filepath.Dir(d)
	}
	return ""
}

// Paths returns every static path literal referenced by file (absolute
// path), always absolute (as the parser resolved it).
func Paths(file string) ([]string, error) {
	parsed, err := parse(file)
	if err != nil {
		return nil, err
	}
	return pathRe.FindAllString(stripStrings(parsed), -1), nil
}

// DynamicPaths returns every dynamic-path base (#27) referenced by file
// (absolute path).
func DynamicPaths(file string) ([]string, error) {
	parsed, err := parse(file)
	if err != nil {
		return nil, err
	}
	return extractDynamicPaths(stripStrings(parsed)), nil
}

// Names returns every name from a recognized "luxos.modules [ ... ]" call
// in file (absolute path). It errors, naming file, on any other use of
// "luxos" (#28) or on a parse failure.
func Names(file string) ([]string, error) {
	parsed, err := parse(file)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to parse: %s", file, lastLine(err.Error()))
	}
	body := stripFormals(parsed)
	names, violations := scanLuxosUses(body)
	if len(violations) > 0 {
		return nil, fmt.Errorf("%s: %s", file, violations[0])
	}
	return names, nil
}

// Validate checks only what the host actually builds: starting from roots
// (the host entrypoint's import paths, relative to modulesDir), it follows
// every luxos.modules name to its unit, transitively, and collects every
// boundary, dynamic-path, call-shape and unresolved-name violation in that
// closure — not just the first. A unit nothing reaches is never checked:
// importing it makes it part of the closure on the next run. A file unit
// contributes itself, a folder unit every *.nix beneath it. err is returned
// for a root that resolves to no unit, or for I/O or parser-lookup failure
// (e.g. nix-instantiate missing); a file that itself fails to parse is
// reported as a Violation instead.
func Validate(modulesDir string, us []units.Unit, roots []string) ([]Violation, error) {
	root := strings.TrimSuffix(modulesDir, "/")

	var queue []string
	seen := make(map[string]bool)
	enqueue := func(unitPath string) {
		if !seen[unitPath] {
			seen[unitPath] = true
			queue = append(queue, unitPath)
		}
	}
	for _, r := range roots {
		unitPath, ok := units.Resolve(us, units.NameFromPath(r))
		if !ok {
			return nil, fmt.Errorf("import ./%s does not resolve to any module under %s/", r, modulesDirName)
		}
		enqueue(unitPath)
	}

	var violations []Violation
	for len(queue) > 0 {
		unitPath := queue[0]
		queue = queue[1:]

		files, err := unitFiles(filepath.Join(root, unitPath))
		if err != nil {
			return nil, err
		}

		for _, file := range files {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				return nil, err
			}

			parsed, perr := parse(file)
			if perr != nil {
				violations = append(violations, Violation{File: rel, Message: "failed to parse"})
				continue
			}

			owner := Owner(root, file)
			stripped := stripStrings(parsed)

			for _, p := range pathRe.FindAllString(stripped, -1) {
				if owner != "" && (p == owner || strings.HasPrefix(p, owner+"/")) {
					continue
				}
				violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("references %s outside its module", p)})
			}

			for _, p := range extractDynamicPaths(stripped) {
				violations = append(violations, Violation{File: rel, Message: fmt.Sprintf("uses a dynamic path (%s)", p)})
			}

			body := stripFormals(parsed)
			names, callViolations := scanLuxosUses(body)
			for _, v := range callViolations {
				violations = append(violations, Violation{File: rel, Message: v})
			}
			for _, name := range names {
				dep, ok := units.Resolve(us, name)
				if !ok {
					violations = append(violations, Violation{
						File:    rel,
						Message: fmt.Sprintf("luxos.modules: '%s' does not resolve to any module under %s/", name, modulesDirName),
					})
					continue
				}
				enqueue(dep)
			}
		}
	}

	sort.SliceStable(violations, func(i, j int) bool { return violations[i].File < violations[j].File })
	return violations, nil
}

// Dependents returns every module file under modulesDir whose
// luxos.modules list contains name. Read-only; a file whose luxos.modules
// use isn't the recognized shape (or that fails to parse) is silently
// skipped — it just can't be a hit.
func Dependents(modulesDir, name string) ([]string, error) {
	root := strings.TrimSuffix(modulesDir, "/")
	files, err := findAllNix(root)
	if err != nil {
		return nil, err
	}
	rootDefault := filepath.Join(root, entrypointName)

	var out []string
	for _, file := range files {
		if file == rootDefault {
			continue
		}
		parsed, err := parse(file)
		if err != nil {
			continue
		}
		names, _ := scanLuxosUses(stripFormals(parsed))
		for _, n := range names {
			if n == name {
				out = append(out, file)
				break
			}
		}
	}
	return out, nil
}

// Retarget is the single writer of module files (#30, §3 "Rewrite"): in
// every module file under modulesDir whose luxos.modules list contains
// name, rewrite it to newName if given, else delete it from the list.
// Every dependent's call shape is validated (Names, which errors on an
// unrecognized use of luxos) before any file is touched, so a bad shape
// anywhere refuses the whole operation before anything changes (item 20).
// Before writing, the candidate is re-parsed and required to equal the
// original parse with exactly that one substitution applied; any other
// difference refuses the write and names the file.
func Retarget(modulesDir, name, newName string) ([]Change, error) {
	files, err := Dependents(modulesDir, name)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}

	// Validate shape everywhere first — Names errors naming the file.
	for _, file := range files {
		if _, err := Names(file); err != nil {
			return nil, err
		}
	}

	var changes []Change
	for _, file := range files {
		srcBytes, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		src := string(srcBytes)

		originalParse, err := parse(file)
		if err != nil {
			return nil, fmt.Errorf("%s failed to parse", file)
		}

		newSrc, ok := rewriteSource(src, name, newName)
		if !ok {
			continue
		}
		expectedParse, ok2 := rewriteParse(originalParse, name, newName)
		if !ok2 {
			continue
		}
		content := newSrc
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}

		tmp, err := os.CreateTemp("", "luxos-refs-*.nix")
		if err != nil {
			return nil, err
		}
		tmpPath := tmp.Name()
		if _, err := tmp.WriteString(content); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return nil, err
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmpPath)
			return nil, err
		}

		actualParse, perr := parse(tmpPath)
		os.Remove(tmpPath)
		if perr != nil {
			return nil, fmt.Errorf("%s: rewrite would fail to parse — refusing to write", file)
		}
		if actualParse != expectedParse {
			return nil, fmt.Errorf("%s: rewrite would change more than the '%s' reference — refusing to write", file, name)
		}

		real, err := filepath.EvalSymlinks(file)
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(real, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(content); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}

		changes = append(changes, Change{File: file, Old: name, New: newName})
	}

	return changes, nil
}

//──[private: parsing helpers]────────────────────────────────────────────────

// stripStrings strips every double-quoted string literal from
// nix-instantiate --parse output (which normalizes all strings, ''indented''
// included, to "..." with backslash escapes), so a path-looking or
// name-looking substring inside a string can never match downstream
// regexes.
func stripStrings(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inStr, esc := false, false
	for _, c := range s {
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			continue
		}
		out.WriteRune(c)
	}
	return out.String()
}

// extractDynamicPaths finds every dynamic-path base in already
// string-stripped parse output.
func extractDynamicPaths(stripped string) []string {
	var out []string
	for _, m := range dynamicRe.FindAllString(stripped, -1) {
		idx := strings.IndexByte(m, '/')
		if idx < 0 {
			continue
		}
		p := m[idx:]
		if sp := strings.IndexByte(p, ' '); sp >= 0 {
			p = p[:sp]
		}
		out = append(out, p)
	}
	return out
}

// stripFormals strips the outer lambda's formals declaration from the start
// of a parse, if present.
func stripFormals(parsed string) string {
	m := formalsRe.FindStringSubmatch(parsed)
	if m == nil {
		return parsed
	}
	return m[3]
}

// scanLuxosUses scans body (a file's parse output, formals already
// stripped) for every recognized luxos.modules call, returning every string
// literal found inside one (names) plus every call-shape or leftover-use
// violation found (violations). Path literals are stripped from the
// leftover-use check first — CONFIG_DIR can legitimately contain the
// substring "luxos" in its own directory name.
func scanLuxosUses(body string) (names []string, violations []string) {
	matches := callSpanRe.FindAllStringSubmatchIndex(body, -1)
	var rest strings.Builder
	last := 0
	for _, m := range matches {
		rest.WriteString(body[last:m[0]])
		inner := body[m[2]:m[3]]
		if !validItemsRe.MatchString(inner) {
			violations = append(violations, "luxos.modules is not called with a literal list of string literals")
		} else {
			for _, im := range parseItemRe.FindAllStringSubmatch(inner, -1) {
				names = append(names, im[1])
			}
		}
		last = m[1]
	}
	rest.WriteString(body[last:])

	remainder := pathRe.ReplaceAllString(rest.String(), "")
	if strings.Contains(remainder, "luxos") {
		violations = append(violations, "uses 'luxos' outside a recognized 'luxos.modules [ ... ]' call")
	}
	return names, violations
}

// lastLine returns the last non-empty line of s.
func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}

//──[private: tree walk]──────────────────────────────────────────────────────

// unitFiles returns the *.nix files a unit consists of: the file itself for
// a file unit, every *.nix beneath it (sorted) for a folder unit.
func unitFiles(unit string) ([]string, error) {
	info, err := os.Stat(unit)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{unit}, nil
	}
	return findAllNix(unit)
}

// findAllNix returns every *.nix file under root (following symlinks, like
// `find -L root -type f -name '*.nix'`), sorted in byte order. A broken
// symlink or vanished entry is skipped.
func findAllNix(root string) ([]string, error) {
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			info, err := os.Stat(p)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			if strings.HasSuffix(e.Name(), ".nix") {
				out = append(out, p)
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

//──[private: retarget's source-level rewrite]───────────────────────────────

// rewriteSource rewrites every matching "luxos.modules [ ... ]" span in
// text (SOURCE, hand formatting) that contains the literal element old: to
// new if given, else drops the element. ok is false when old wasn't found
// in any span (text is then unchanged and must be ignored by the caller).
func rewriteSource(text, old, newName string) (rewritten string, ok bool) {
	return rewriteCalls(text, old, newName, sourceSpanRe, sourceItemRe, "plain")
}

// rewriteParse applies the identical rewrite to a file's PARSE output
// instead of its source, so the two can be cross-checked.
func rewriteParse(text, old, newName string) (rewritten string, ok bool) {
	return rewriteCalls(text, old, newName, callSpanRe, parseItemRe, "parens")
}

// rewriteCalls rewrites every matching span in text that contains the
// literal element old, using spanRe/itemRe to find spans/items and style
// ("plain" -> "tok", "parens" -> ("tok")) to reconstruct them. Spans with no
// occurrence of old are left byte-for-byte, including their original
// formatting.
func rewriteCalls(text, old, newName string, spanRe, itemRe *regexp.Regexp, style string) (string, bool) {
	var needle string
	if style == "parens" {
		needle = `("` + old + `")`
	} else {
		needle = `"` + old + `"`
	}

	matches := spanRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return "", false
	}

	var out strings.Builder
	last := 0
	changed := false
	for _, m := range matches {
		spanStart, spanEnd := m[0], m[1]
		innerStart, innerEnd := m[2], m[3]
		out.WriteString(text[last:spanStart])
		span := text[spanStart:spanEnd]

		if strings.Contains(span, needle) {
			changed = true
			inner := text[innerStart:innerEnd]

			var items []string
			for _, im := range itemRe.FindAllStringSubmatch(inner, -1) {
				tok := im[1]
				if tok == old {
					if newName != "" {
						items = append(items, itemStr(style, newName))
					}
					continue
				}
				items = append(items, itemStr(style, tok))
			}

			head := "luxos.modules"
			if style == "parens" {
				head = "(luxos).modules"
			}
			if len(items) > 0 {
				out.WriteString(head + " [ " + strings.Join(items, " ") + " ]")
			} else {
				out.WriteString(head + " [ ]")
			}
		} else {
			out.WriteString(span)
		}
		last = spanEnd
	}
	out.WriteString(text[last:])

	if !changed {
		return "", false
	}
	return out.String(), true
}

// itemStr renders one list element in the given style.
func itemStr(style, tok string) string {
	if style == "parens" {
		return `("` + tok + `")`
	}
	return `"` + tok + `"`
}
