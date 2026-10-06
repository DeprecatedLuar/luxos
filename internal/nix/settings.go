package nix

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// evalSettingsExpr reports every assignment of each settings file, cut at
// the requested attribute depth, with the line it starts on. Values are
// never forced past an attribute set, and function arguments are stubbed to
// throw, so a file setting a package value is read like any other.
const evalSettingsExpr = `
{ requestsJson }:
let
  requests = builtins.fromJSON requestsJson;
  leaves = depth: prefix: set: builtins.concatMap (n:
    let
      v = set.${n};
      p = prefix ++ [ n ];
      deeper = depth > 1 && (builtins.tryEval (builtins.isAttrs v)).value == true;
    in
    if deeper then leaves (depth - 1) p v
    else [ { path = p; line = (builtins.unsafeGetAttrPos n set).line; } ]) (builtins.attrNames set);
  readFile = req:
    let
      m = import req.path;
      r = if builtins.isFunction m
        then m (builtins.mapAttrs (n: _: throw "luxos-stub:${n}") (builtins.functionArgs m))
        else m;
    in leaves req.depth [ ] r;
in builtins.listToAttrs (map (req: { name = req.file; value = readFile req; }) requests)
`

const (
	settingsIndent  = "  "
	commentPrefix   = "# "
	trailingComment = " # "
	closingLine     = "}"
	attrPathSep     = "."
)

// assignmentLineRe splits a whole single-line assignment into its statement,
// up to the first ";" that only a comment follows, and that comment.
var assignmentLineRe = regexp.MustCompile(`^(.*?;)\s*(#.*)?$`)

// SettingsRequest asks for the assignments of File down to Depth attribute levels.
type SettingsRequest struct {
	File  string `json:"file"`
	Depth int    `json:"depth"`
}

type settingsEval struct {
	evalFile
	Depth int `json:"depth"`
}

// SettingLeaf is one assignment of a settings file and the line it starts on.
type SettingLeaf struct {
	Path []string `json:"path"`
	Line int      `json:"line"`
}

// Setting is one value to write into a settings file, with the comment
// that ends its line.
type Setting struct {
	Path    []string
	Value   any
	Comment string
}

// ReadSettings returns each requested file's assignments sorted by line,
// keyed by absolute path. A file nix cannot evaluate is left out and returned
// in broken with nix's message. Every file must exist.
func ReadSettings(reqs []SettingsRequest) (map[string][]SettingLeaf, map[string]string, error) {
	files := make([]string, len(reqs))
	for i, r := range reqs {
		files[i] = r.File
	}
	abs, err := absExisting("nix.ReadSettings", files)
	if err != nil {
		return nil, nil, err
	}
	depth := map[string]int{}
	for i, a := range abs {
		depth[a] = reqs[i].Depth
	}

	out := map[string][]SettingLeaf{}
	broken, err := evalEach(abs, func(batch []string) error {
		if len(batch) == 0 {
			return nil
		}
		files, err := evalFiles(batch)
		if err != nil {
			return err
		}
		batchReqs := make([]settingsEval, len(files))
		for i, f := range files {
			batchReqs[i] = settingsEval{evalFile: f, Depth: depth[f.File]}
		}
		payload, err := json.Marshal(batchReqs)
		if err != nil {
			return err
		}
		raw, err := EvalJSON(evalSettingsExpr, map[string]string{"requestsJson": string(payload)})
		if err != nil {
			return err
		}
		got := map[string][]SettingLeaf{}
		if err := json.Unmarshal(raw, &got); err != nil {
			return fmt.Errorf("nix.ReadSettings: parse nix-instantiate output: %w", err)
		}
		for _, leaves := range got {
			sort.SliceStable(leaves, func(i, j int) bool { return leaves[i].Line < leaves[j].Line })
		}
		out = got
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, broken, nil
}

// RenderSettings renders a new settings file holding settings, in order.
func RenderSettings(settings []Setting) []byte {
	var b strings.Builder
	b.WriteString("{\n")
	for _, s := range settings {
		b.WriteString(settingLine(s))
	}
	b.WriteString(closingLine + "\n")
	return []byte(b.String())
}

func settingLine(s Setting) string {
	names := make([]string, len(s.Path))
	for i, n := range s.Path {
		names[i] = nixAttrName(n)
	}
	line := fmt.Sprintf("%s%s = %s;", settingsIndent, strings.Join(names, attrPathSep), renderNixValue(s.Value, 1))
	if !strings.Contains(line, "\n") {
		line += lineComment(s.Comment)
	}
	return line + "\n"
}

// lineComment is comment as the tail of a line, on one line, or "" when empty.
func lineComment(comment string) string {
	words := strings.Fields(comment)
	if len(words) == 0 {
		return ""
	}
	return trailingComment + strings.Join(words, " ")
}

// AppendSettings adds settings before the last line of file that is only "}".
func AppendSettings(file string, settings []Setting) error {
	lines, err := readLines(file)
	if err != nil {
		return err
	}
	end, err := closingIndex(file, lines)
	if err != nil {
		return err
	}
	var add []string
	for _, s := range settings {
		add = append(add, strings.TrimSuffix(settingLine(s), "\n"))
	}
	out := append(append(append([]string{}, lines[:end]...), add...), lines[end:]...)
	return writeLines(file, out)
}

// CommentSettings comments out the assignments of file at paths drop: each
// one from its first line up to the line before the next assignment, or
// before the closing "}".
func CommentSettings(file string, depth int, drop [][]string) error {
	read, broken, err := ReadSettings([]SettingsRequest{{File: file, Depth: depth}})
	if err != nil {
		return err
	}
	for f, msg := range broken {
		return fmt.Errorf("%s: %s", f, msg)
	}
	var leaves []SettingLeaf
	for _, l := range read {
		leaves = l
	}
	lines, err := readLines(file)
	if err != nil {
		return err
	}
	end, err := closingIndex(file, lines)
	if err != nil {
		return err
	}
	dropped := map[string]bool{}
	for _, p := range drop {
		dropped[strings.Join(p, attrPathSep)] = true
	}
	for i, l := range leaves {
		if !dropped[strings.Join(l.Path, attrPathSep)] {
			continue
		}
		stop := end
		for _, next := range leaves[i+1:] {
			if next.Line > l.Line {
				stop = next.Line - 1
				break
			}
		}
		first := lines[l.Line-1]
		base := first[:len(first)-len(strings.TrimLeft(first, " \t"))]
		for n := l.Line - 1; n < stop; n++ {
			lines[n] = base + commentPrefix + strings.TrimPrefix(lines[n], base)
		}
	}
	return writeLines(file, lines)
}

// CommentSettingLines sets the trailing comment of each single-line
// assignment in leaves to comments[key], removing it when the key has none.
// Multi-line assignments are left alone. The file is written only when a line
// changes.
func CommentSettingLines(file string, leaves []SettingLeaf, comments map[string]string) error {
	lines, err := readLines(file)
	if err != nil {
		return err
	}
	changed := false
	for _, l := range leaves {
		m := assignmentLineRe.FindStringSubmatch(lines[l.Line-1])
		if m == nil || strings.Count(m[1], `"`)%2 != 0 {
			continue
		}
		line := m[1] + lineComment(comments[strings.Join(l.Path, attrPathSep)])
		if line != lines[l.Line-1] {
			lines[l.Line-1] = line
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeLines(file, lines)
}

// closingIndex is the index of the last line of lines that is only "}".
func closingIndex(file string, lines []string) (int, error) {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == closingLine {
			return i, nil
		}
	}
	return 0, fmt.Errorf("%s: no line holding only '}' to write before", file)
}
