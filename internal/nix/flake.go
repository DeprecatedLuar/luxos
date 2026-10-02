package nix

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const flakeBin = "nix"

// writeFlakeArgs runs flake-file's write-flake app. --no-write-lock-file
// keeps the bootstrap's default nixpkgs from moving a lock pinned to another
// one; flakeLockArgs then fills in whatever is missing.
var writeFlakeArgs = []string{"run", "--no-write-lock-file", ".#write-flake"}

// flakeLockArgs locks missing inputs without moving existing pins.
var flakeLockArgs = []string{"flake", "lock"}

const nixConfigVar = "NIX_CONFIG"

// flakeFeatures enables flakes for luxos's own nix calls, so the first
// rebuild on a machine whose nix.conf has them off (before system.nix
// turns them on) still works. extra- appends, so it is a no-op elsewhere.
const flakeFeatures = "extra-experimental-features = nix-command flakes"

// failedAttrFormat is how nix names the batch entry an evaluation failed in.
const failedAttrFormat = `while evaluating attribute '"%s"'`

// nixErrorPrefix starts the line of nix's own message, after its trace.
const nixErrorPrefix = "error: "

// FlakeUpdate runs `nix flake update <inputs...> --flake <flakeDir>`, passing
// stdout and stderr through to the caller's. With no inputs every input moves.
func FlakeUpdate(flakeDir string, inputs ...string) error {
	args := append([]string{"flake", "update"}, inputs...)
	args = append(args, "--flake", flakeDir)
	return shell.Run(shell.Cmd{Bin: flakeBin, Args: args, Env: flakeEnv(os.Environ())})
}

// BuildFlakeRef builds the flake reference ref and returns its store path.
// It tries --offline first; a reference that is not yet realized fails there
// and is retried once with the network allowed.
func BuildFlakeRef(ref string) (string, error) {
	out, err := buildFlakeRef(ref, true)
	if err != nil {
		out, err = buildFlakeRef(ref, false)
	}
	return out, err
}

func buildFlakeRef(ref string, offline bool) (string, error) {
	args := []string{"build", ref}
	if offline {
		args = append(args, "--offline")
	}
	args = append(args, "--no-link", "--print-out-paths")
	return storePath(shell.Cmd{Bin: flakeBin, Args: args, Env: flakeEnv(os.Environ())})
}

// WriteFlake runs flake-file's write-flake app in stagingDir, regenerating
// flake.nix from the input declarations in the staged modules.
func WriteFlake(stagingDir string) error {
	return runIn(stagingDir, writeFlakeArgs)
}

// FlakeLock runs `nix flake lock` in stagingDir: missing inputs are locked,
// existing pins stay where they are.
func FlakeLock(stagingDir string) error {
	return runIn(stagingDir, flakeLockArgs)
}

func runIn(dir string, args []string) error {
	_, err := shell.Output(shell.Cmd{Bin: flakeBin, Args: args, Dir: dir, Env: flakeEnv(os.Environ())})
	return err
}

// flakeEnv returns env with flakeFeatures added to NIX_CONFIG, keeping any
// settings already there.
func flakeEnv(env []string) []string {
	prefix := nixConfigVar + "="
	out := make([]string, 0, len(env)+1)
	value := flakeFeatures
	for _, kv := range env {
		if existing, ok := strings.CutPrefix(kv, prefix); ok {
			if existing != "" {
				value = existing + "\n" + flakeFeatures
			}
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+value)
}

// evalInputsExpr reads flake-file.inputs from each file in a batch by
// evaluating it, not parsing it: a module's value can be either the flat
// `flake-file.inputs.name.url = "..."` form or the nested
// `flake-file.inputs.name = { url = ...; inputs.nixpkgs.follows = ...; }`
// form, and both desugar to the same attribute set, so one reader covers
// both. Every function argument is stubbed to throw, so a file needing a
// real one (a callPackage file, say) yields no declarations instead of
// failing the batch; deepSeq forces that throw to fire inside tryEval even
// when it is nested inside a declared value.
const evalInputsExpr = `
{ filesJson }:
let
  files = builtins.fromJSON filesJson;
  readFile = file:
    let
      ev = builtins.tryEval (
        let
          m = import file;
          isFn = builtins.isFunction m;
          args = if isFn then
            builtins.mapAttrs (n: _: throw "luxos-stub:${n}") (builtins.functionArgs m)
          else {};
          r = if isFn then m args else m;
          decls = if builtins.isAttrs r then (r.flake-file.inputs or {}) else {};
        in builtins.deepSeq decls decls
      );
      inputs = if ev.success then ev.value else {};
    in
      builtins.mapAttrs (name: value: {
        inherit value;
        pos = builtins.unsafeGetAttrPos name inputs;
      }) inputs;
in
  builtins.listToAttrs (map (file: { name = file; value = readFile file; }) files)
`

type evalPos struct {
	Line int `json:"line"`
}

type evalDecl struct {
	Value map[string]any `json:"value"`
	Pos   *evalPos       `json:"pos"`
}

// InputDecl is one flake input declared in a module's source: its name, its
// full value (Value, e.g. {"url": "...", "inputs": {"nixpkgs": {"follows":
// "nixpkgs"}}}), and where it was written.
type InputDecl struct {
	File  string
	Name  string
	URL   string
	Line  int
	Value map[string]any
}

// InputDecls returns the flake-file.inputs declarations across files, in
// file-then-name order. A file that nix cannot evaluate is an error naming
// it; see ReadInputDecls. Every file must exist.
func InputDecls(files ...string) ([]InputDecl, error) {
	decls, broken, err := ReadInputDecls(files...)
	if err != nil {
		return nil, err
	}
	if len(broken) > 0 {
		lines := make([]string, 0, len(broken))
		for file, msg := range broken {
			lines = append(lines, file+": "+msg)
		}
		sort.Strings(lines)
		return nil, errors.New(strings.Join(lines, "\n"))
	}
	return decls, nil
}

// ReadInputDecls returns the flake-file.inputs declarations across files, in
// file-then-name order, evaluating each file so a value built at runtime (not
// a plain literal) is read like any other. A file that needs a real argument
// contributes none. A file nix cannot evaluate at all (syntax or type error)
// is left out and returned in broken, keyed by absolute path, with nix's
// message; each one costs one more evaluation. Every file must exist.
func ReadInputDecls(files ...string) ([]InputDecl, map[string]string, error) {
	abs := make([]string, len(files))
	for i, f := range files {
		a, err := filepath.Abs(f)
		if err != nil {
			return nil, nil, err
		}
		if _, err := os.Stat(a); err != nil {
			return nil, nil, fmt.Errorf("nix.InputDecls: %w", err)
		}
		abs[i] = a
	}

	broken := map[string]string{}
	for {
		decls, err := evalInputDecls(abs)
		if err == nil {
			return decls, broken, nil
		}
		file := failedFile(err, abs)
		if file == "" {
			return nil, nil, err
		}
		broken[file] = nixMessage(err)
		abs = slices.DeleteFunc(abs, func(f string) bool { return f == file })
	}
}

// evalInputDecls evaluates absolute, existing files in one nix-instantiate run.
func evalInputDecls(abs []string) ([]InputDecl, error) {
	if len(abs) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(abs)
	if err != nil {
		return nil, err
	}
	out, err := EvalJSON(evalInputsExpr, map[string]string{"filesJson": string(payload)})
	if err != nil {
		return nil, err
	}

	var raw map[string]map[string]evalDecl
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("nix.InputDecls: parse nix-instantiate output: %w", err)
	}

	var decls []InputDecl
	for _, file := range abs {
		names := make([]string, 0, len(raw[file]))
		for name := range raw[file] {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			d := raw[file][name]
			line := 0
			if d.Pos != nil {
				line = d.Pos.Line
			}
			url, _ := d.Value["url"].(string)
			decls = append(decls, InputDecl{File: file, Name: name, URL: url, Line: line, Value: d.Value})
		}
	}
	return decls, nil
}

// failedFile returns the file of files whose evaluation err reports, or ""
// when err names none of them.
func failedFile(err error, files []string) string {
	text := err.Error()
	for _, f := range files {
		if strings.Contains(text, fmt.Sprintf(failedAttrFormat, f)) {
			return f
		}
	}
	return ""
}

// nixMessage returns nix's own message from err: its last "error: " line.
func nixMessage(err error) string {
	lines := strings.Split(err.Error(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if msg, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), nixErrorPrefix); ok {
			return msg
		}
	}
	return strings.TrimSpace(err.Error())
}

const BaseChannelInput = "nixpkgs"

var ErrNoBaseChannel = errors.New("missing base channel")

// BaseChannel returns the url and 1-based line of the one nixpkgs declaration
// in file. None is ErrNoBaseChannel; more than one is an error naming the
// lines (only reachable through a merged, not literally duplicated,
// attribute set — Nix itself refuses two literal definitions of the same
// attribute).
func BaseChannel(file string) (url string, line int, err error) {
	decls, err := InputDecls(file)
	if err != nil {
		return "", 0, err
	}
	var found []InputDecl
	for _, d := range decls {
		if d.Name == BaseChannelInput {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return "", 0, ErrNoBaseChannel
	case 1:
		return found[0].URL, found[0].Line, nil
	}
	lines := make([]string, len(found))
	for i, d := range found {
		lines[i] = strconv.Itoa(d.Line)
	}
	return "", 0, fmt.Errorf("%s: base channel declared %d times (lines %s); keep one", file, len(found), strings.Join(lines, ", "))
}

// nixIdentRe matches a bare Nix identifier, safe to write unquoted as an
// attribute name.
var nixIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_'-]*$`)

// RenderInputs renders decls as the body of a flake.nix `inputs = { ... };`
// block, one line per name (a name declared more than once keeps its first
// declaration; flake-file's own evaluation judges any real conflict),
// sorted by name for a stable, diff-friendly file.
func RenderInputs(decls []InputDecl) string {
	seen := map[string]bool{}
	var names []string
	values := map[string]map[string]any{}
	for _, d := range decls {
		if seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		names = append(names, d.Name)
		values[d.Name] = d.Value
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		v := values[name]
		if url, ok := v["url"].(string); ok && len(v) == 1 {
			fmt.Fprintf(&b, "    %s.url = %s;\n", nixAttrName(name), nixString(url))
			continue
		}
		fmt.Fprintf(&b, "    %s = %s;\n", nixAttrName(name), renderNixValue(v, 2))
	}
	return b.String()
}

// nixAttrName renders name as a Nix attribute name: bare when it is a valid
// identifier, quoted otherwise.
func nixAttrName(name string) string {
	if nixIdentRe.MatchString(name) {
		return name
	}
	return nixString(name)
}

// renderNixValue renders a decoded JSON value (string, bool, float64 or
// nested map[string]any, as builtins.toJSON produces from a Nix attrset) as
// Nix source, indented at depth levels of two spaces.
func renderNixValue(v any, depth int) string {
	switch val := v.(type) {
	case string:
		return nixString(val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)
	case map[string]any:
		if len(val) == 0 {
			return "{ }"
		}
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pad := strings.Repeat("  ", depth)
		inner := strings.Repeat("  ", depth+1)
		var b strings.Builder
		b.WriteString("{\n")
		for _, k := range keys {
			fmt.Fprintf(&b, "%s%s = %s;\n", inner, nixAttrName(k), renderNixValue(val[k], depth+1))
		}
		b.WriteString(pad)
		b.WriteString("}")
		return b.String()
	default:
		return "null"
	}
}

func nixString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\', '$':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
