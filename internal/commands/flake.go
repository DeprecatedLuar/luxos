package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/heal"
	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/nixsrc"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/refs"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/units"
	"github.com/DeprecatedLuar/luxos/internal/upstream"
)

// flakeFlagSpec is the flag spec passed to shared.Parse.
const flakeFlagSpec = "machine:value config|C:value"

// flakeListFlagSpec is flakeFlagSpec plus the listing's own flags.
const flakeListFlagSpec = flakeFlagSpec + " offline:bool"

const (
	// flakeInputsLabel is the root label of the input tree.
	flakeInputsLabel = "inputs/"

	// flakeFileInput is flake-file's own input: rev-pinned in the embedded
	// bootstrap, never actionable, never listed.
	flakeFileInput = "flake-file"

	// flakeNoteBehind marks an input whose upstream tip differs from its
	// locked rev; flakeNoteUnknown one whose tip could not be determined.
	flakeNoteBehind  = "↑"
	flakeNoteUnknown = "?"
)

const (
	// flakeSharedDisplayDir and flakeLocalDisplayDir prefix a declaring
	// file's path relative to CONFIG_DIR (`modules` and the `local` link).
	flakeSharedDisplayDir = "modules"
	flakeLocalDisplayDir  = "local/modules"

	flakeSourceDefaultBranch = " (default branch)"
	flakeDeclaredBuiltIn     = "built in"
	flakeDeclaredPulledIn    = "pulled in by "
	flakeDeclaredGone        = "no longer declared (the next rebuild prunes it)"
	flakeNotLocked           = "not locked yet (the next rebuild locks it)"
	flakeUpToDate            = "  up to date"
	flakeCommitsFormat       = "  +%d commits"
	flakeShortRevLen         = 7
	flakePullsInSep          = ", "
	flakePathSep             = "/"
)

// flakeBuiltinInputs are declared by the embedded bootstrap flake, so they
// are declared even when no module says so, and always sit at the tree root.
var flakeBuiltinInputs = []string{"luxos", "nixpkgs"}

// Flake implements `luxos flake`: list|ls, update and `<name>` (the
// single-input view). A flag-like first argument prints the help page.
func Flake(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", "flake"})
	}

	switch args[0] {
	case "list", "ls":
		return flakeList(args[1:])
	case "update":
		return flakeUpdate(args[1:])
	default:
		if strings.HasPrefix(args[0], "-") {
			return help.Run([]string{"help", "flake"})
		}
		return flakeShow(args[0], args[1:])
	}
}

// flakeUpdate escalates to root, stages the host's tree, runs
// `nix flake update` on it and copies the lock back to the host folder.
func flakeUpdate(args []string) error {
	if err := shared.EnsureRoot(append([]string{"flake", "update"}, args...)); err != nil {
		return err
	}

	opts, names, err := shared.Parse(flakeFlagSpec, args)
	if err != nil {
		return err
	}

	p, host, hostDir, err := resolveFlakeHost(opts)
	if err != nil {
		return err
	}

	if err := heal.Run(os.Stdout, p, host, false); err != nil {
		return err
	}

	if err := nix.FlakeUpdate(p.Staging, names...); err != nil {
		return err
	}

	hostLock := filepath.Join(hostDir, flakeLockName)
	if err := staging.CopyLockBack(p.Staging, hostLock); err != nil {
		return err
	}

	fmt.Printf("  updated: %s\n", hostLock)
	return nil
}

// resolveFlakeHost applies --config, resolves the paths, the host name
// (--machine, else the hostname) and that host's folder.
func resolveFlakeHost(opts map[string]string) (p paths.Paths, host, hostDir string, err error) {
	if opts["config"] != "" {
		abs, err := filepath.Abs(opts["config"])
		if err != nil {
			return p, "", "", err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return p, "", "", fmt.Errorf("config dir %s does not exist", abs)
		}
		if err := os.Setenv(configDirEnv, abs); err != nil {
			return p, "", "", err
		}
	}

	host = opts["machine"]
	if host == "" {
		host, err = os.Hostname()
		if err != nil {
			return p, "", "", err
		}
	}

	p, err = paths.Resolve()
	if err != nil {
		return p, "", "", err
	}

	hostDir, err = config.ResolveHost(p.Machines, host)
	if err != nil {
		return p, "", "", err
	}
	return p, host, hostDir, nil
}

//──[list]─────────────────────────────────────────────────────────────────

// flakeList implements `flake list|ls`: the host's inputs as a tree. It only
// reads; no root needed.
func flakeList(args []string) error {
	opts, _, err := shared.Parse(flakeListFlagSpec, args)
	if err != nil {
		return err
	}

	p, _, hostDir, err := resolveFlakeHost(opts)
	if err != nil {
		return err
	}

	decls, err := flakeDeclarations(p, hostDir)
	if err != nil {
		return err
	}
	graph, err := staging.ReadLockGraph(filepath.Join(hostDir, flakeLockName))
	if err != nil {
		return err
	}

	var notes map[string]string
	if opts["offline"] == "" {
		notes = flakeUpstreamNotes(graph)
	}
	rows := flakeBuildRows(decls, graph, notes)

	tty := stdoutIsTTY()
	pal := treePalette{}
	if colorsEnabled(tty) {
		pal = colorTreePalette
	}

	var out strings.Builder
	if tty {
		moduleRenderTTY(&out, rows, flakeInputsLabel, pal)
	} else {
		moduleRenderPlain(&out, rows)
	}
	fmt.Print(out.String())
	return nil
}

// flakeUpstreamNotes checks the upstream tip of every locked node reachable
// from the root (flake-file excluded), one goroutine per node, and returns
// node key -> note: flakeNoteBehind when the tip differs from the locked rev,
// flakeNoteUnknown on any failure, absent when equal.
func flakeUpstreamNotes(graph staging.LockGraph) map[string]string {
	rootInputs := graph.Nodes[staging.LockRootNode].Inputs
	keys := map[string]bool{}
	var walk func(key string)
	walk = func(key string) {
		if keys[key] {
			return
		}
		keys[key] = true
		for _, target := range graph.Nodes[key].Inputs {
			walk(target)
		}
	}
	for name, key := range rootInputs {
		if name != flakeFileInput {
			walk(key)
		}
	}
	delete(keys, staging.LockRootNode)

	notes := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for key := range keys {
		node := graph.Nodes[key]
		if node.Locked.Rev == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			note := ""
			tip, err := upstream.Tip(node.Original)
			switch {
			case err != nil:
				note = flakeNoteUnknown
			case tip != node.Locked.Rev:
				note = flakeNoteBehind
			}
			if note != "" {
				mu.Lock()
				notes[key] = note
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return notes
}

// flakeDecl is one `flake-file.inputs.<name>.url` definition.
type flakeDecl struct {
	unit string // unit path that holds it
	file string // file path relative to CONFIG_DIR
	line int
	url  string
}

// flakeDeclarations returns, per input name, the sorted unit paths that
// declare it, over the host's selected units and the units they pull in.
func flakeDeclarations(p paths.Paths, hostDir string) (map[string][]string, error) {
	sites, err := flakeDeclSites(p, hostDir)
	if err != nil {
		return nil, err
	}
	return flakeUnitsByInput(sites), nil
}

// flakeUnitsByInput reduces declaration sites to the sorted declaring unit
// paths per input name.
func flakeUnitsByInput(sites map[string][]flakeDecl) map[string][]string {
	out := make(map[string][]string, len(sites))
	for input, decls := range sites {
		set := map[string]bool{}
		for _, d := range decls {
			if !set[d.unit] {
				set[d.unit] = true
				out[input] = append(out[input], d.unit)
			}
		}
		sort.Strings(out[input])
	}
	return out
}

// flakeDeclSites returns, per input name, every declaring definition over the
// host's selected units and the units they pull in, sorted by file then line.
func flakeDeclSites(p paths.Paths, hostDir string) (map[string][]flakeDecl, error) {
	localModules := filepath.Join(hostDir, "modules")

	us, err := units.Walk(p.Modules, localModules)
	if err != nil {
		return nil, err
	}
	selection, err := imports.List(filepath.Join(hostDir, "modules.nix"))
	if err != nil {
		return nil, err
	}
	pulled, err := refs.Closure(p.Modules, us, selection)
	if err != nil {
		return nil, err
	}

	names := map[string]bool{}
	for _, sel := range selection {
		names[units.NameFromPath(sel)] = true
	}
	for name := range pulled {
		names[name] = true
	}

	out := map[string][]flakeDecl{}
	for name := range names {
		unitPath, ok := units.Resolve(us, name)
		if !ok {
			continue
		}
		root, rel, display := p.Modules, unitPath, flakeSharedDisplayDir
		if strings.HasPrefix(unitPath, localPathPrefix) {
			root, rel, display = localModules, strings.TrimPrefix(unitPath, localPathPrefix), flakeLocalDisplayDir
		}
		files, err := refs.UnitFiles(filepath.Join(root, rel))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			fileDecls, err := nixsrc.InputDecls(file)
			if err != nil {
				return nil, err
			}
			fileRel, err := filepath.Rel(root, file)
			if err != nil {
				return nil, err
			}
			for _, d := range fileDecls {
				out[d.Name] = append(out[d.Name], flakeDecl{
					unit: unitPath,
					file: filepath.ToSlash(filepath.Join(display, fileRel)),
					line: d.Line,
					url:  d.URL,
				})
			}
		}
	}
	for _, decls := range out {
		sort.Slice(decls, func(i, j int) bool {
			if decls[i].file != decls[j].file {
				return decls[i].file < decls[j].file
			}
			return decls[i].line < decls[j].line
		})
	}
	return out, nil
}

// flakeBuildRows builds the input tree's rows from declarations (input name
// -> declaring unit paths) and the lock graph. Pure given its inputs.
// notes maps a lock node key to the row note for that node (nil: no notes).
func flakeBuildRows(decls map[string][]string, graph staging.LockGraph, notes map[string]string) []moduleRow {
	builtin := map[string]bool{}
	declared := map[string]bool{}
	for _, name := range flakeBuiltinInputs {
		builtin[name] = true
		declared[name] = true
	}
	for name := range decls {
		declared[name] = true
	}

	locked := map[string]bool{}
	for _, name := range graph.Root {
		locked[name] = true
	}

	names := map[string]bool{}
	for name := range declared {
		names[name] = true
	}
	for name := range locked {
		names[name] = true
	}
	delete(names, flakeFileInput)

	rootInputs := graph.Nodes[staging.LockRootNode].Inputs

	var rows []moduleRow
	for name := range names {
		marker := markerRunningOnly
		switch {
		case declared[name] && locked[name]:
			marker = markerEnabledBoth
		case declared[name]:
			marker = markerEnabledOnly
		}

		var category []string
		if !builtin[name] && len(decls[name]) == 1 {
			if dir := filepath.Dir(decls[name][0]); dir != "." {
				category = strings.Split(dir, "/")
			}
		}

		var children []moduleRow
		if locked[name] {
			children = flakeTransitiveRows(graph, rootInputs[name], map[string]bool{rootInputs[name]: true}, notes)
		}

		var note string
		if locked[name] {
			note = notes[rootInputs[name]]
		}
		rows = append(rows, moduleRow{
			category: category,
			name:     name,
			marker:   marker,
			rank:     moduleMarkerRank(marker),
			children: children,
			note:     note,
		})
	}
	return rows
}

// flakeTransitiveRows returns the inputs of lock node key as pulled-in rows,
// recursing without depth limit. ancestors holds the node keys on the path
// from the root input, so a cyclic lock cannot recurse forever.
func flakeTransitiveRows(graph staging.LockGraph, key string, ancestors map[string]bool, notes map[string]string) []moduleRow {
	var rows []moduleRow
	for name, target := range graph.Nodes[key].Inputs {
		if ancestors[target] {
			continue
		}
		ancestors[target] = true
		rows = append(rows, moduleRow{
			name:     name,
			marker:   markerPulled,
			rank:     moduleMarkerRank(markerPulled),
			children: flakeTransitiveRows(graph, target, ancestors, notes),
			note:     notes[target],
		})
		delete(ancestors, target)
	}
	return rows
}

//──[show]─────────────────────────────────────────────────────────────────

// flakeUpstream is what the network told us about one input: the tip, the
// tags, and the locked..tip range. Each part carries its own error so one
// failed request renders `?` for its value only.
type flakeUpstream struct {
	tip        string
	tipErr     error
	tags       map[string]string
	tagsErr    error
	ahead      int
	shas       []string // oldest first
	compareErr error
}

// flakeLine is one line of the single-input view; children nest beneath it.
type flakeLine struct {
	label    string
	value    string
	dim      string // dimmed suffix after value
	children []flakeLine
}

// flakeView is the single-input view: a header and its lines.
type flakeView struct {
	marker string
	name   string
	note   string
	lines  []flakeLine
}

// flakeShow implements `flake <name>`: where the input comes from and what
// updating it would give. It only reads; no root needed.
func flakeShow(name string, args []string) error {
	opts, extra, err := shared.Parse(flakeListFlagSpec, args)
	if err != nil {
		return err
	}
	if len(extra) > 0 {
		return fmt.Errorf("unexpected argument '%s'\n  usage: luxos flake <name> [--machine <name>] [--config|-C <dir>] [--offline]", extra[0])
	}

	p, host, hostDir, err := resolveFlakeHost(opts)
	if err != nil {
		return err
	}
	sites, err := flakeDeclSites(p, hostDir)
	if err != nil {
		return err
	}
	graph, err := staging.ReadLockGraph(filepath.Join(hostDir, flakeLockName))
	if err != nil {
		return err
	}

	var fetch func(staging.LockRef, string) *flakeUpstream
	if opts["offline"] == "" {
		fetch = flakeFetchUpstream
	}
	view, err := flakeBuildView(name, host, sites, graph, fetch)
	if err != nil {
		return err
	}

	pal := treePalette{}
	if colorsEnabled(stdoutIsTTY()) {
		pal = colorTreePalette
	}
	var out strings.Builder
	flakeRenderView(&out, view, pal)
	fmt.Print(out.String())
	return nil
}

// flakeFetchUpstream asks the network about ref's tip, tags and the range
// from locked to the tip.
func flakeFetchUpstream(ref staging.LockRef, locked string) *flakeUpstream {
	up := &flakeUpstream{}
	up.tip, up.tipErr = upstream.Tip(ref)
	up.tags, up.tagsErr = upstream.Tags(ref)
	if up.tipErr == nil && up.tip != locked {
		up.ahead, up.shas, up.compareErr = upstream.Compare(ref, locked, up.tip)
	}
	return up
}

// flakeBuildView resolves name (a root input or a tree path) against the lock
// graph and declarations and builds its view. fetch is nil offline.
func flakeBuildView(name, host string, sites map[string][]flakeDecl, graph staging.LockGraph,
	fetch func(staging.LockRef, string) *flakeUpstream) (flakeView, error) {

	unknown := fmt.Errorf("no input '%s' for %s\n  list them with: luxos flakes", name, host)

	segments := strings.Split(name, flakePathSep)
	root := segments[0]
	if root == flakeFileInput {
		return flakeView{}, unknown
	}

	builtin := false
	for _, b := range flakeBuiltinInputs {
		builtin = builtin || b == root
	}
	rootKey, locked := graph.Nodes[staging.LockRootNode].Inputs[root]
	declared := builtin || len(sites[root]) > 0
	if !locked && !declared {
		return flakeView{}, unknown
	}

	view := flakeView{name: segments[len(segments)-1]}
	key, hasNode := rootKey, locked
	switch {
	case locked && declared:
		view.marker = markerEnabledBoth
	case declared:
		view.marker = markerEnabledOnly
	default:
		view.marker = markerRunningOnly
	}

	parent, prev := "", root
	for _, seg := range segments[1:] {
		next, ok := graph.Nodes[key].Inputs[seg]
		if !locked || !ok {
			return flakeView{}, unknown
		}
		parent, prev, key = prev, seg, next
		view.marker = markerPulled
	}

	var node staging.LockNode
	if hasNode {
		node = graph.Nodes[key]
	}

	if source := flakeSourceText(node.Original, sites[root], hasNode); source != "" {
		view.lines = append(view.lines, flakeLine{label: "source", value: source})
	}
	view.lines = append(view.lines, flakeDeclaredLine(parent, sites[root], builtin, view.marker))

	versionLine, note := flakeVersionLine(node, hasNode, fetch)
	view.note = note
	view.lines = append(view.lines, versionLine)

	if names := flakeSortedKeys(node.Inputs); len(names) > 0 {
		view.lines = append(view.lines, flakeLine{label: "pulls in", children: []flakeLine{
			{value: strings.Join(names, flakePullsInSep)},
		}})
	}
	return view, nil
}

// flakeSourceText renders the node's original as a flake ref, with the
// default-branch note when it names no ref. An unlocked input falls back to
// its first declared url; with neither it is empty.
func flakeSourceText(orig staging.LockRef, decls []flakeDecl, hasNode bool) string {
	if !hasNode {
		if len(decls) > 0 {
			return decls[0].url
		}
		return ""
	}
	text := orig.URL
	switch orig.Type {
	case "github":
		text = "github:" + orig.Owner + "/" + orig.Repo
		if orig.Ref != "" {
			text += "/" + orig.Ref
		}
	case "git":
	default:
		if text == "" {
			text = orig.Type
		}
		return text
	}
	if orig.Ref == "" {
		text += flakeSourceDefaultBranch
	}
	return text
}

// flakeDeclaredLine is the `declared` line: pulled-in parent, the declaring
// definitions (inline when one, children when several), built in, or gone.
func flakeDeclaredLine(parent string, decls []flakeDecl, builtin bool, marker string) flakeLine {
	line := flakeLine{label: "declared"}
	switch {
	case marker == markerPulled:
		line.value = flakeDeclaredPulledIn + parent
	case len(decls) == 1:
		line.value = fmt.Sprintf("%s:%d", decls[0].file, decls[0].line)
	case len(decls) > 1:
		for _, d := range decls {
			line.children = append(line.children, flakeLine{value: fmt.Sprintf("%s:%d", d.file, d.line)})
		}
	case builtin:
		line.value = flakeDeclaredBuiltIn
	default:
		line.value = flakeDeclaredGone
	}
	return line
}

// flakeVersionLine builds the `version` line and the header note. fetch nil
// (offline) shows the current rev only.
func flakeVersionLine(node staging.LockNode, hasNode bool, fetch func(staging.LockRef, string) *flakeUpstream) (flakeLine, string) {
	line := flakeLine{label: "version"}
	if !hasNode {
		line.children = []flakeLine{{label: "current", value: flakeNotLocked}}
		return line, ""
	}

	rev := node.Locked.Rev
	var up *flakeUpstream
	if fetch != nil && rev != "" {
		up = fetch(node.Original, rev)
	}

	current := flakeShortRev(rev)
	if up != nil {
		if tag, ok := up.tags[rev]; ok {
			current = tag
		}
	}
	cur := flakeLine{label: "current", value: current}
	if up == nil {
		line.children = []flakeLine{cur}
		return line, ""
	}

	switch {
	case up.tipErr != nil:
		line.children = []flakeLine{cur, {label: "latest", value: flakeNoteUnknown}}
		return line, flakeNoteUnknown
	case up.tip == rev:
		cur.value += flakeUpToDate
		line.children = []flakeLine{cur}
		return line, ""
	}

	latest := flakeLine{label: "latest"}
	switch {
	case errors.Is(up.compareErr, upstream.ErrUnsupported):
		// No range to search: an exact tag on the tip or the tip itself.
		latest.value = flakeShortRev(up.tip)
		if tag, ok := up.tags[up.tip]; ok {
			latest.value = tag
		}
	case up.compareErr != nil || (up.tagsErr != nil && !errors.Is(up.tagsErr, upstream.ErrUnsupported)):
		latest.value = flakeNoteUnknown
	default:
		latest.value = flakeShortRev(up.tip)
		for i := len(up.shas) - 1; i >= 0; i-- {
			if tag, ok := up.tags[up.shas[i]]; ok {
				latest.value = tag
				break
			}
		}
		if up.ahead > 0 {
			latest.dim = fmt.Sprintf(flakeCommitsFormat, up.ahead)
		}
	}
	line.children = []flakeLine{cur, latest}
	return line, flakeNoteBehind
}

func flakeShortRev(rev string) string {
	if len(rev) > flakeShortRevLen {
		return rev[:flakeShortRevLen]
	}
	return rev
}

func flakeSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// flakeRenderView writes the view as a tree with the palette and connectors
// of `module list`; an empty palette renders the same text without color.
func flakeRenderView(w *strings.Builder, v flakeView, pal treePalette) {
	fmt.Fprintf(w, "%s%s %s%s", markerColor(pal, v.marker), v.marker, v.name, pal.reset)
	if v.note != "" {
		fmt.Fprintf(w, " %s%s%s", noteColor(pal, v.note), v.note, pal.reset)
	}
	w.WriteString("\n")
	flakeRenderLines(w, v.lines, "", pal)
}

// flakeRenderLines prints siblings with connectors, padding labels to the
// widest sibling label plus two spaces so values align.
func flakeRenderLines(w *strings.Builder, lines []flakeLine, prefix string, pal treePalette) {
	width := 0
	for _, l := range lines {
		if len(l.label) > width {
			width = len(l.label)
		}
	}
	for i, l := range lines {
		connector, childPrefix := "├── ", prefix+"│   "
		if i == len(lines)-1 {
			connector, childPrefix = "└── ", prefix+"    "
		}
		fmt.Fprintf(w, "%s%s%s%s", pal.line, prefix, connector, pal.reset)
		if l.label != "" {
			fmt.Fprintf(w, "%s%s%s", pal.title, l.label, pal.reset)
			if l.value != "" {
				w.WriteString(strings.Repeat(" ", width-len(l.label)+2))
			}
		}
		w.WriteString(l.value)
		if l.dim != "" {
			fmt.Fprintf(w, "%s%s%s", pal.off, l.dim, pal.reset)
		}
		w.WriteString("\n")
		flakeRenderLines(w, l.children, childPrefix, pal)
	}
}
