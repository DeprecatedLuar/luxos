package nixsrc

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
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

// ModuleNames returns every name from a recognized luxos.modules call in file,
// plus every call-shape violation found. err is a parse failure only.
func ModuleNames(file string) (names, violations []string, err error) {
	parsed, err := parse(file)
	if err != nil {
		return nil, nil, err
	}
	names, violations = scanLuxosUses(stripFormals(parsed))
	return names, violations, nil
}

// Names is ModuleNames with the first violation returned as an error naming
// file. It errors, naming file, on any other use of "luxos" (#28) or on a
// parse failure.
func Names(file string) ([]string, error) {
	names, violations, err := ModuleNames(file)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to parse: %s", file, lastLine(err.Error()))
	}
	if len(violations) > 0 {
		return nil, fmt.Errorf("%s: %s", file, violations[0])
	}
	return names, nil
}

// RetargetModuleName rewrites name to newName (or drops it when newName == "")
// in every luxos.modules call in file. changed is false when name was not found.
// The candidate's parse must equal the original parse with that one substitution,
// else it refuses and names file. Writes through Write.
func RetargetModuleName(file, name, newName string) (changed bool, err error) {
	srcBytes, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}

	originalParse, err := parse(file)
	if err != nil {
		return false, fmt.Errorf("%s failed to parse", file)
	}

	newSrc, ok := rewriteSource(string(srcBytes), name, newName)
	if !ok {
		return false, nil
	}
	expectedParse, ok := rewriteParse(originalParse, name, newName)
	if !ok {
		return false, nil
	}
	content := newSrc
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	tmp, err := os.CreateTemp("", tempPattern)
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}

	actualParse, perr := parse(tmpPath)
	if perr != nil {
		return false, fmt.Errorf("%s: rewrite would fail to parse — refusing to write", file)
	}
	if actualParse != expectedParse {
		return false, fmt.Errorf("%s: rewrite would change more than the '%s' reference — refusing to write", file, name)
	}

	if err := Write(file, []byte(content)); err != nil {
		return false, err
	}
	return true, nil
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
