package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/heal"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/nixsrc"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/refs"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/upstream"
)

const flakeFlagSpec = "machine:value config|C:value"

const flakeUpdateTmpPattern = "luxos-flake-update-"

const flakeListFlagSpec = flakeFlagSpec + " offline:bool raw:bool json:bool"

const (
	flakeInputsLabel = "inputs/"

	// Rev-pinned in the embedded bootstrap, never actionable, never listed.
	flakeFileInput = "flake-file"

	flakeNoteBehind  = "↑"
	flakeNoteUnknown = "?"

	flakeStatusBehind  = "behind"
	flakeStatusCurrent = "current"
	flakeStatusUnknown = "unknown"

	flakeViewCommitsCurrent = "0"

	flakeViewListSep = " "
)

const (
	flakeSharedDisplayDir = "modules"
	flakeLocalDisplayDir  = "local/modules"

	flakeMachinesDisplayDir = ".local/machines"

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
	flakeViewSep             = "\n"
)

// Declared by the embedded bootstrap flake, so they are declared even when no
// module says so, and always sit at the tree root.
var flakeBuiltinInputs = []string{"luxos", "nixpkgs"}

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

func flakeUpdate(args []string) (err error) {
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

	tmp, err := os.MkdirTemp("", flakeUpdateTmpPattern)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
	p.Staging = tmp

	// Staging progress is rebuild's output; failures come back as errors.
	if err := heal.Run(io.Discard, p, host, false); err != nil {
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

func flakeList(args []string) error {
	opts, _, err := shared.Parse(flakeListFlagSpec, args)
	if err != nil {
		return err
	}

	if opts["json"] != "" && opts["raw"] != "" {
		return errJSONConflict("--raw")
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
	switch {
	case opts["json"] != "":
		if err := flakeRenderListJSON(&out, rows); err != nil {
			return err
		}
	case tty && opts["raw"] == "":
		moduleRenderTTY(&out, rows, flakeInputsLabel, pal)
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
	return writeJSON(w, entries)
}

func flakeRenderPlain(w *strings.Builder, rows []moduleRow) {
	for _, e := range flakeListEntries(rows) {
		fmt.Fprintln(w, strings.Join([]string{e.Path, e.State, e.Status}, plainColumnSep))
	}
}

// Unknown when offline, not locked, or its check failed; behind or current otherwise.
func flakeStatus(locked, online bool, note string) string {
	switch {
	case !locked || !online || note == flakeNoteUnknown:
		return flakeStatusUnknown
	case note == flakeNoteBehind:
		return flakeStatusBehind
	default:
		return flakeStatusCurrent
	}
}

// Checks the upstream tip of every locked node reachable from the root
// (flake-file excluded), one goroutine per node. Returns node key -> note:
// flakeNoteBehind when the tip differs from the locked rev, flakeNoteUnknown
// on any failure, absent when equal.
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

type flakeDecl struct {
	unit string
	file string
	line int
	url  string
}

func flakeDeclarations(p paths.Paths, hostDir string) (map[string][]string, error) {
	sites, err := flakeDeclSites(p, hostDir)
	if err != nil {
		return nil, err
	}
	return flakeUnitsByInput(sites), nil
}

func flakeUnitsByInput(sites map[string][]flakeDecl) map[string][]string {
	out := make(map[string][]string, len(sites))
	for input, decls := range sites {
		set := map[string]bool{}
		for _, d := range decls {
			if d.unit != "" && !set[d.unit] {
				set[d.unit] = true
				out[input] = append(out[input], d.unit)
			}
		}
		sort.Strings(out[input])
	}
	return out
}

func flakeDeclSites(p paths.Paths, hostDir string) (map[string][]flakeDecl, error) {
	localModules := filepath.Join(hostDir, "modules")

	sel, err := refs.SelectedUnits(p.Modules, localModules, hostDir)
	if err != nil {
		return nil, err
	}

	out := map[string][]flakeDecl{}
	for _, u := range sel {
		display := flakeSharedDisplayDir
		if u.Root == localModules {
			display = flakeLocalDisplayDir
		}
		fileDecls, err := nixsrc.InputDecls(u.Files...)
		if err != nil {
			return nil, err
		}
		for _, d := range fileDecls {
			fileRel, err := filepath.Rel(u.Root, d.File)
			if err != nil {
				return nil, err
			}
			out[d.Name] = append(out[d.Name], flakeDecl{
				unit: u.Path,
				file: filepath.ToSlash(filepath.Join(display, fileRel)),
				line: d.Line,
				url:  d.URL,
			})
		}
	}
	base, err := flakeBaseChannelDecl(hostDir)
	if err != nil {
		return nil, err
	}
	if base != nil {
		out[nixsrc.BaseChannelInput] = append(out[nixsrc.BaseChannelInput], *base)
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

// Nil when the host's machine.nix has no base channel (the rebuild preflight
// reports that). Belongs to no unit.
func flakeBaseChannelDecl(hostDir string) (*flakeDecl, error) {
	url, line, err := nixsrc.BaseChannel(filepath.Join(hostDir, config.MachineFile))
	if errors.Is(err, nixsrc.ErrNoBaseChannel) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rel := filepath.Join(flakeMachinesDisplayDir, filepath.Base(hostDir), config.MachineFile)
	return &flakeDecl{file: filepath.ToSlash(rel), line: line, url: url}, nil
}

// Builds the input tree's rows from declarations (input name -> declaring unit
// paths) and the lock graph. notes maps a lock node key to the row note for
// that node (nil: no notes).
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
			status:   flakeStatus(locked[name], notes != nil, note),
		})
	}
	return rows
}

// Recurses without depth limit. ancestors holds the node keys on the path from
// the root input, so a cyclic lock cannot recurse forever.
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
			status:   flakeStatus(true, notes != nil, notes[target]),
		})
		delete(ancestors, target)
	}
	return rows
}

//──[show]─────────────────────────────────────────────────────────────────

// Each part carries its own error so one failed request renders `?` for its
// value only.
type flakeUpstream struct {
	tip        string
	tipErr     error
	tags       map[string]string
	tagsErr    error
	ahead      int
	shas       []string // oldest first
	compareErr error
}

type flakeLine struct {
	label    string
	value    string
	dim      string
	children []flakeLine
}

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
	lines  []flakeLine
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
		return errJSONConflict("--raw")
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
	var views []flakeView
	for _, name := range names {
		found, err := flakeViewsFor(name, host, sites, graph, fetch)
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

	tty := stdoutIsTTY()
	pal := treePalette{}
	if colorsEnabled(tty) {
		pal = colorTreePalette
	}
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

func flakeViewsFor(name, host string, sites map[string][]flakeDecl, graph staging.LockGraph,
	fetch func(staging.LockRef, string) *flakeUpstream) ([]flakeView, error) {

	var views []flakeView
	rootView, rootErr := flakeBuildView(name, host, sites, graph, fetch)
	if rootErr == nil {
		views = append(views, rootView)
	}
	if !strings.Contains(name, flakePathSep) {
		for _, path := range flakeNestedPaths(graph, name) {
			view, err := flakeBuildView(path, host, sites, graph, fetch)
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

func flakeNestedPaths(graph staging.LockGraph, name string) []string {
	var found []string
	var walk func(key, prefix string, ancestors map[string]bool)
	walk = func(key, prefix string, ancestors map[string]bool) {
		inputs := graph.Nodes[key].Inputs
		for _, child := range flakeSortedKeys(inputs) {
			target := inputs[child]
			if ancestors[target] || (prefix == "" && child == flakeFileInput) {
				continue
			}
			path := child
			if prefix != "" {
				path = prefix + flakePathSep + child
				if child == name {
					found = append(found, path)
				}
			}
			ancestors[target] = true
			walk(target, path, ancestors)
			delete(ancestors, target)
		}
	}
	walk(staging.LockRootNode, "", map[string]bool{})
	return found
}

func flakeFetchUpstream(ref staging.LockRef, locked string) *flakeUpstream {
	up := &flakeUpstream{}
	up.tip, up.tipErr = upstream.Tip(ref)
	up.tags, up.tagsErr = upstream.Tags(ref)
	if up.tipErr == nil && up.tip != locked {
		up.ahead, up.shas, up.compareErr = upstream.Compare(ref, locked, up.tip)
	}
	return up
}

// fetch is nil offline.
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

	source := flakeSourceText(node.Original, sites[root], hasNode)
	if source != "" {
		view.lines = append(view.lines, flakeLine{label: "source", value: source})
	}
	view.lines = append(view.lines, flakeDeclaredLine(parent, sites[root], builtin, view.marker))

	versionLine, note, version := flakeVersionLine(node, hasNode, fetch)
	view.note = note
	view.lines = append(view.lines, versionLine)

	names := flakeSortedKeys(node.Inputs)
	if len(names) > 0 {
		view.lines = append(view.lines, flakeLine{label: "pulls in", children: []flakeLine{
			{value: strings.Join(names, flakePullsInSep)},
		}})
	}

	var declaredSites []string
	if view.marker != markerPulled {
		for _, d := range sites[root] {
			declaredSites = append(declaredSites, fmt.Sprintf("%s:%d", d.file, d.line))
		}
	}
	view.data = flakeViewData{
		name:     name,
		state:    markerWord(view.marker),
		status:   flakeStatus(hasNode && version.checked, true, note),
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

// checked is false when no upstream answer was obtained (offline, not locked).
type flakeVersion struct {
	current string
	latest  string
	commits string
	checked bool
}

// fetch nil (offline) shows the current rev only.
func flakeVersionLine(node staging.LockNode, hasNode bool, fetch func(staging.LockRef, string) *flakeUpstream) (flakeLine, string, flakeVersion) {
	line := flakeLine{label: "version"}
	if !hasNode {
		line.children = []flakeLine{{label: "current", value: flakeNotLocked}}
		return line, "", flakeVersion{}
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
	version := flakeVersion{current: current}
	if up == nil {
		line.children = []flakeLine{cur}
		return line, "", version
	}
	version.checked = true

	switch {
	case up.tipErr != nil:
		line.children = []flakeLine{cur, {label: "latest", value: flakeNoteUnknown}}
		version.latest = flakeStatusUnknown
		return line, flakeNoteUnknown, version
	case up.tip == rev:
		cur.value += flakeUpToDate
		line.children = []flakeLine{cur}
		version.commits = flakeViewCommitsCurrent
		return line, "", version
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
	version.latest = latest.value
	if latest.value == flakeNoteUnknown {
		version.latest = flakeStatusUnknown
	}
	if up.compareErr == nil {
		version.commits = strconv.Itoa(up.ahead)
	}
	return line, flakeNoteBehind, version
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
		return writeJSON(w, objs[0])
	}
	return writeJSON(w, objs)
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

// An empty palette renders the same text without color.
func flakeRenderView(w *strings.Builder, v flakeView, pal treePalette) {
	fmt.Fprintf(w, "%s%s %s%s", markerColor(pal, v.marker), v.marker, v.name, pal.reset)
	if v.note != "" {
		fmt.Fprintf(w, " %s%s%s", noteColor(pal, v.note), v.note, pal.reset)
	}
	w.WriteString("\n")
	flakeRenderLines(w, v.lines, "", pal)
}

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
