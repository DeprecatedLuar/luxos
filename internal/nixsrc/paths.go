package nixsrc

import (
	"regexp"
	"strings"
)

var (
	// A path token as nix-instantiate --parse prints it: starts with "/"
	// followed by a non-"/" char (so the "//" update operator never
	// matches; division prints as __div and <nixpkgs> as __findFile, so
	// neither yields a path either).
	pathRe = regexp.MustCompile(`/[A-Za-z0-9._+-][A-Za-z0-9._+/-]*`)

	// The static base of a concatenation ("(<path> + <expr>)"), including
	// the bare root "/" base that pathRe deliberately excludes.
	dynamicRe = regexp.MustCompile(`(^|[ ([])/([A-Za-z0-9._+-][A-Za-z0-9._+/-]*)? \+ `)
)

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

// PathsAndDynamic returns Paths and DynamicPaths of file from one parse.
func PathsAndDynamic(file string) (paths, dynamic []string, err error) {
	parsed, err := parse(file)
	if err != nil {
		return nil, nil, err
	}
	stripped := stripStrings(parsed)
	return pathRe.FindAllString(stripped, -1), extractDynamicPaths(stripped), nil
}

// stripStrings strips every double-quoted string literal from
// nix-instantiate --parse output (which normalizes all strings, ”indented”
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
