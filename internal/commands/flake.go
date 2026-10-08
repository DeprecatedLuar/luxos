package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/flake"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const flakeFlagSpec = "machine:value config|C:value"

const flakeUpdateTmpPattern = "luxos-flake-update-"

const (
	updateFlagSpec      = flakeFlagSpec + " yes|y:bool"
	updateDefaultInput  = "luxos"
	updateDefaultPrompt = "Update luxos? [y/N] "
)

var errUpdateAborted = errors.New("aborted: nothing updated\n  confirm with: luxos update --yes")

const flakeListFlagSpec = flakeFlagSpec + " offline:bool raw:bool json:bool"

const gitLockType = "git"

const (
	flakeInputsLabel = "inputs/"

	flakeViewCommitsCurrent = "0"

	flakeViewListSep = " "
)

const (
	flakeSourceDefaultBranch = " (default branch)"
	flakeDeclaredBuiltIn     = "built in"
	flakeDeclaredPulledIn    = "pulled in by "
	flakeDeclaredGone        = "no longer declared (the next rebuild prunes it)"
	flakeNotLocked           = "not locked yet (the next rebuild locks it)"
	flakeUpToDate            = "  up to date"
	flakeCommitsFormat       = "  +%d commits"
	flakePullsInSep          = ", "
	flakePathSep             = "/"
	flakeViewSep             = "\n"
)

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
		return flakeShow(args)
	}
}

// Update is `flake update`; with no inputs it updates luxos after a confirmation.
func Update(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		return help.Run([]string{"help", "update"})
	}

	opts, names, err := shared.Parse(updateFlagSpec, args)
	if err != nil {
		return err
	}

	if len(names) == 0 {
		if opts["yes"] == "" {
			reply, err := ui.Ask(os.Stderr, updateDefaultPrompt)
			if err != nil {
				return err
			}
			if !ui.IsYes(reply) {
				return errUpdateAborted
			}
		}
		names = []string{updateDefaultInput}
	}

	return updateInputs(opts, names)
}

func flakeUpdate(args []string) error {
	opts, names, err := shared.Parse(flakeFlagSpec, args)
	if err != nil {
		return err
	}
	return updateInputs(opts, names)
}

func updateInputs(opts map[string]string, names []string) error {
	p, host, hostDir, err := shared.ResolveHost(opts)
	if err != nil {
		return err
	}

	return withTempStage(p, host, flakeUpdateTmpPattern, func(tmp string) error {
		if err := nix.FlakeUpdate(tmp, names...); err != nil {
			return err
		}
		hostLock := filepath.Join(hostDir, flakeLockName)
		if _, err := config.CopyLockBack(filepath.Join(tmp, flakeLockName), hostLock); err != nil {
			return err
		}
		fmt.Printf("  updated: %s\n", hostLock)
		return nil
	})
}

//──[list]─────────────────────────────────────────────────────────────────

func flakeList(args []string) error {
	opts, _, err := shared.Parse(flakeListFlagSpec, args)
	if err != nil {
		return err
	}

	if opts["json"] != "" && opts["raw"] != "" {
		return ui.ErrJSONConflict("--raw")
	}

	p, _, hostDir, err := shared.ResolveHost(opts)
	if err != nil {
		return err
	}

	h, err := modules.Load(p.Modules, hostDir)
	if err != nil {
		return err
	}
	sites, err := flake.Declarations(h)
	if err != nil {
		return err
	}
	decls := flake.Units(sites)
	graph, err := nix.ReadLock(filepath.Join(hostDir, flakeLockName))
	if err != nil {
		return err
	}

	var notes map[string]flake.Note
	if opts["offline"] == "" {
		notes = flake.Notes(flake.GitHub{}, graph)
	}
	rows := flakeBuildRows(decls, graph, notes)

	tty := ui.IsTerminal(os.Stdout)
	pal := ui.PaletteFor(os.Stdout)

	var out strings.Builder
	switch {
	case opts["json"] != "":
		if err := flakeRenderListJSON(&out, rows); err != nil {
			return err
		}
	case tty && opts["raw"] == "":
		ui.Tree(&out, uiRows(rows), flakeInputsLabel, pal)
	default:
		flakeRenderPlain(&out, rows)
	}
	fmt.Print(out.String())
	return nil
}

type flakeListJSONRow struct {
	Path   string `json:"path"`
	State  string `json:"state"`
	Status string `json:"status"`
}

func flakeListEntries(rows []moduleRow) []flakeListJSONRow {
	var entries []flakeListJSONRow
	var add func(prefix string, rows []moduleRow)
	add = func(prefix string, rows []moduleRow) {
		for _, r := range rows {
			addressed := prefix + r.name
			path := addressed
			if prefix == "" {
				path = rowPath(r)
			}
			entries = append(entries, flakeListJSONRow{path, markerWord(r.marker), r.status})
			add(addressed+flakePathSep, r.children)
		}
	}
	add("", rows)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func flakeRenderListJSON(w *strings.Builder, rows []moduleRow) error {
	entries := flakeListEntries(rows)
	if entries == nil {
		entries = []flakeListJSONRow{}
	}
	return ui.JSON(w, entries)
}

func flakeRenderPlain(w *strings.Builder, rows []moduleRow) {
	entries := flakeListEntries(rows)
	lines := make([][]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, []string{e.Path, e.State, e.Status})
	}
	ui.Plain(w, lines)
}

// Builds the input tree's rows from declarations (input name -> declaring unit
// paths) and the lock graph. notes maps a lock node key to the row note for
// that node (nil: no notes).
func flakeBuildRows(decls map[string][]string, graph nix.Lock, notes map[string]flake.Note) []moduleRow {
	builtin := map[string]bool{}
	declared := map[string]bool{}
	for _, name := range flake.Builtin {
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
	delete(names, flake.FileInput)

	rootInputs := graph.Nodes[nix.LockRootNode].Inputs

	var rows []moduleRow
	for name := range names {
		marker := inputMarker(declared[name], locked[name])

		var category []string
		if !builtin[name] && len(decls[name]) == 1 {
			if dir := filepath.Dir(decls[name][0]); dir != "." {
				category = strings.Split(dir, "/")
			}
		}

		var children []moduleRow
		var note flake.Note
		if locked[name] {
			children = flakeTransitiveRows(graph, rootInputs[name], map[string]bool{rootInputs[name]: true}, notes)
			note = notes[rootInputs[name]]
		}
		rows = append(rows, moduleRow{
			category: category,
			name:     name,
			marker:   marker,
			rank:     moduleMarkerRank(marker),
			children: children,
			note:     string(note),
			status:   string(flake.StatusOf(locked[name], notes != nil, note)),
		})
	}
	return rows
}

// inputMarker is the state marker of a root input.
func inputMarker(declared, locked bool) string {
	switch {
	case declared && locked:
		return markerEnabledBoth
	case declared:
		return markerEnabledOnly
	default:
		return markerRunningOnly
	}
}

// Recurses without depth limit. ancestors holds the node keys on the path from
// the root input, so a cyclic lock cannot recurse forever.
func flakeTransitiveRows(graph nix.Lock, key string, ancestors map[string]bool, notes map[string]flake.Note) []moduleRow {
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
			note:     string(notes[target]),
			status:   string(flake.StatusOf(true, notes != nil, notes[target])),
		})
		delete(ancestors, target)
	}
	return rows
}

// flakeRenderView draws v as a headed tree; an empty palette renders the same
// text without color.
func flakeRenderView(w *strings.Builder, v flakeView, pal ui.Palette) {
	ui.RenderView(w, ui.View{
		Marker: v.marker, Color: markerColor(v.marker), Name: v.name,
		Note: v.note, NoteColor: noteColor(v.note), Lines: v.lines,
	}, pal)
}

//──[show]─────────────────────────────────────────────────────────────────

type flakeViewData struct {
	name     string
	state    string
	status   string
	source   string
	declared []string
	current  string
	latest   string
	commits  string
	pulls    []string
}

type flakeView struct {
	marker string
	name   string
	note   string
	lines  []ui.Line
	data   flakeViewData
}

func flakeShow(args []string) error {
	opts, names, err := shared.Parse(flakeListFlagSpec, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("missing input name\n  usage: luxos flake <name>... [--machine <name>] [--config|-C <dir>] [--offline] [--raw|--json]")
	}
	if opts["json"] != "" && opts["raw"] != "" {
		return ui.ErrJSONConflict("--raw")
	}

	p, host, hostDir, err := shared.ResolveHost(opts)
	if err != nil {
		return err
	}
	h, err := modules.Load(p.Modules, hostDir)
	if err != nil {
		return err
	}
	sites, err := flake.Declarations(h)
	if err != nil {
		return err
	}
	graph, err := nix.ReadLock(filepath.Join(hostDir, flakeLockName))
	if err != nil {
		return err
	}

	var up flake.Upstream
	if opts["offline"] == "" {
		up = flake.GitHub{}
	}
	var views []flakeView
	for _, name := range names {
		found, err := flakeViewsFor(name, host, sites, graph, up)
		if err != nil {
			return err
		}
		views = append(views, found...)
	}

	if opts["json"] != "" {
		var out strings.Builder
		if err := flakeRenderViewsJSON(&out, views); err != nil {
			return err
		}
		fmt.Print(out.String())
		return nil
	}

	tty := ui.IsTerminal(os.Stdout)
	pal := ui.PaletteFor(os.Stdout)
	var out strings.Builder
	for i, view := range views {
		if i > 0 {
			out.WriteString(flakeViewSep)
		}
		if tty && opts["raw"] == "" {
			flakeRenderView(&out, view, pal)
		} else {
			flakeRenderViewPlain(&out, view.data)
		}
	}
	fmt.Print(out.String())
	return nil
}

func flakeViewsFor(name, host string, sites map[string][]flake.Decl, graph nix.Lock, up flake.Upstream) ([]flakeView, error) {
	var views []flakeView
	rootView, rootErr := flakeBuildView(name, host, sites, graph, up)
	if rootErr == nil {
		views = append(views, rootView)
	}
	if !strings.Contains(name, flakePathSep) {
		for _, path := range flake.NestedPaths(graph, name) {
			view, err := flakeBuildView(path, host, sites, graph, up)
			if err != nil {
				return nil, err
			}
			view.name = path
			views = append(views, view)
		}
	}
	if len(views) == 0 {
		return nil, rootErr
	}
	return views, nil
}

// up is nil offline.
func flakeBuildView(name, host string, sites map[string][]flake.Decl, graph nix.Lock, up flake.Upstream) (flakeView, error) {
	unknown := fmt.Errorf("no input '%s' for %s\n  list them with: luxos flakes", name, host)

	segments := strings.Split(name, flakePathSep)
	root := segments[0]
	if root == flake.FileInput {
		return flakeView{}, unknown
	}

	builtin := slices.Contains(flake.Builtin, root)
	rootKey, locked := graph.Nodes[nix.LockRootNode].Inputs[root]
	declared := builtin || len(sites[root]) > 0
	if !locked && !declared {
		return flakeView{}, unknown
	}

	view := flakeView{name: segments[len(segments)-1], marker: inputMarker(declared, locked)}
	key, hasNode := rootKey, locked

	parent, prev := "", root
	for _, seg := range segments[1:] {
		next, ok := graph.Nodes[key].Inputs[seg]
		if !locked || !ok {
			return flakeView{}, unknown
		}
		parent, prev, key = prev, seg, next
		view.marker = markerPulled
	}

	var node nix.LockNode
	if hasNode {
		node = graph.Nodes[key]
	}

	source := flakeSourceText(node.Original, sites[root], hasNode)
	if source != "" {
		view.lines = append(view.lines, ui.Line{Label: "source", Value: source})
	}
	view.lines = append(view.lines, flakeDeclaredLine(parent, sites[root], builtin, view.marker))

	versionLine, note, version := flakeVersionLine(node, hasNode, up)
	view.note = note
	view.lines = append(view.lines, versionLine)

	names := flakeSortedKeys(node.Inputs)
	if len(names) > 0 {
		view.lines = append(view.lines, ui.Line{Label: "pulls in", Children: []ui.Line{
			{Value: strings.Join(names, flakePullsInSep)},
		}})
	}

	var declaredSites []string
	if view.marker != markerPulled {
		for _, d := range sites[root] {
			declaredSites = append(declaredSites, fmt.Sprintf("%s:%d", d.File, d.Line))
		}
	}
	view.data = flakeViewData{
		name:     name,
		state:    markerWord(view.marker),
		status:   string(flake.StatusOf(hasNode && version.checked, true, flake.Note(note))),
		source:   strings.TrimSuffix(source, flakeSourceDefaultBranch),
		declared: declaredSites,
		current:  version.current,
		latest:   version.latest,
		commits:  version.commits,
		pulls:    names,
	}
	return view, nil
}

// Appends the default-branch note when it names no ref. An unlocked input falls
// back to its first declared url; with neither it is empty.
func flakeSourceText(orig nix.LockRef, decls []flake.Decl, hasNode bool) string {
	if !hasNode {
		if len(decls) > 0 {
			return decls[0].URL
		}
		return ""
	}
	var text string
	switch orig.Type {
	case githubLockType:
		text = githubLockType + ":" + orig.Owner + "/" + orig.Repo
		if orig.Ref != "" {
			text += "/" + orig.Ref
		}
	case gitLockType:
		text = orig.URL
	default:
		if orig.URL != "" {
			return orig.URL
		}
		return orig.Type
	}
	if orig.Ref == "" {
		text += flakeSourceDefaultBranch
	}
	return text
}

func flakeDeclaredLine(parent string, decls []flake.Decl, builtin bool, marker string) ui.Line {
	line := ui.Line{Label: "declared"}
	switch {
	case marker == markerPulled:
		line.Value = flakeDeclaredPulledIn + parent
	case len(decls) == 1:
		line.Value = fmt.Sprintf("%s:%d", decls[0].File, decls[0].Line)
	case len(decls) > 1:
		for _, d := range decls {
			line.Children = append(line.Children, ui.Line{Value: fmt.Sprintf("%s:%d", d.File, d.Line)})
		}
	case builtin:
		line.Value = flakeDeclaredBuiltIn
	default:
		line.Value = flakeDeclaredGone
	}
	return line
}

// checked is false when no upstream answer was obtained (offline, not locked).
type flakeVersion struct {
	current string
	latest  string
	commits string
	checked bool
}

// flakeVersionLine renders a node's version from flake.Version; a nil up
// (offline) shows the current rev only.
func flakeVersionLine(node nix.LockNode, hasNode bool, up flake.Upstream) (ui.Line, string, flakeVersion) {
	line := ui.Line{Label: "version"}
	if !hasNode {
		line.Children = []ui.Line{{Label: "current", Value: flakeNotLocked}}
		return line, "", flakeVersion{}
	}

	info := flake.Version(up, node)
	cur := ui.Line{Label: "current", Value: info.Current}
	version := flakeVersion{current: info.Current}
	if !info.Checked {
		line.Children = []ui.Line{cur}
		return line, "", version
	}
	version.checked = true

	unknown := string(flake.NoteUnknown)
	switch info.Note {
	case flake.NoteUnknown:
		line.Children = []ui.Line{cur, {Label: "latest", Value: unknown}}
		version.latest = string(flake.StatusUnknown)
		return line, unknown, version
	case "":
		cur.Value += flakeUpToDate
		line.Children = []ui.Line{cur}
		version.commits = flakeViewCommitsCurrent
		return line, "", version
	}

	latest := ui.Line{Label: "latest", Value: info.Latest}
	if info.HasAhead && info.Ahead > 0 && info.Latest != unknown {
		latest.Dim = fmt.Sprintf(flakeCommitsFormat, info.Ahead)
	}
	line.Children = []ui.Line{cur, latest}
	version.latest = info.Latest
	if info.Latest == unknown {
		version.latest = string(flake.StatusUnknown)
	}
	if info.HasAhead {
		version.commits = strconv.Itoa(info.Ahead)
	}
	return line, string(flake.NoteBehind), version
}

func flakeRenderViewPlain(w *strings.Builder, d flakeViewData) {
	for _, kv := range [][2]string{
		{"name", d.name}, {"state", d.state}, {"status", d.status}, {"source", d.source},
		{"declared", strings.Join(d.declared, flakeViewListSep)}, {"current", d.current}, {"latest", d.latest},
		{"commits", d.commits}, {"pulls", strings.Join(d.pulls, flakeViewListSep)},
	} {
		fmt.Fprintf(w, "%s=%s\n", kv[0], kv[1])
	}
}

// current, latest and commits are null when empty (not checked, or not known).
type flakeViewJSON struct {
	Name     string   `json:"name"`
	State    string   `json:"state"`
	Status   string   `json:"status"`
	Source   string   `json:"source"`
	Declared []string `json:"declared"`
	Pulls    []string `json:"pulls"`
	Current  *string  `json:"current"`
	Latest   *string  `json:"latest"`
	Commits  *int     `json:"commits"`
}

func flakeViewToJSON(d flakeViewData) (flakeViewJSON, error) {
	out := flakeViewJSON{
		Name: d.name, State: d.state, Status: d.status, Source: d.source,
		Declared: append([]string{}, d.declared...), Pulls: append([]string{}, d.pulls...),
	}
	if d.current != "" {
		out.Current = &d.current
	}
	if d.latest != "" {
		out.Latest = &d.latest
	}
	if d.commits != "" {
		n, err := strconv.Atoi(d.commits)
		if err != nil {
			return out, fmt.Errorf("commit count %q of input %s: %w", d.commits, d.name, err)
		}
		out.Commits = &n
	}
	return out, nil
}

func flakeRenderViewsJSON(w *strings.Builder, views []flakeView) error {
	objs := make([]flakeViewJSON, 0, len(views))
	for _, v := range views {
		obj, err := flakeViewToJSON(v.data)
		if err != nil {
			return err
		}
		objs = append(objs, obj)
	}
	if len(objs) == 1 {
		return ui.JSON(w, objs[0])
	}
	return ui.JSON(w, objs)
}

func flakeSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
