package commands

import (
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

// flakeBuiltinInputs are declared by the embedded bootstrap flake, so they
// are declared even when no module says so, and always sit at the tree root.
var flakeBuiltinInputs = []string{"luxos", "nixpkgs"}

// Flake implements `luxos flake`: list|ls and update. Any other first
// argument prints the help page.
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
		return help.Run([]string{"help", "flake"})
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

// flakeDeclarations returns, per input name, the sorted unit paths that
// declare it, over the host's selected units and the units they pull in.
func flakeDeclarations(p paths.Paths, hostDir string) (map[string][]string, error) {
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

	declaredBy := map[string]map[string]bool{}
	for name := range names {
		unitPath, ok := units.Resolve(us, name)
		if !ok {
			continue
		}
		root, rel := p.Modules, unitPath
		if strings.HasPrefix(unitPath, localPathPrefix) {
			root, rel = localModules, strings.TrimPrefix(unitPath, localPathPrefix)
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
			for _, d := range fileDecls {
				if declaredBy[d.Name] == nil {
					declaredBy[d.Name] = map[string]bool{}
				}
				declaredBy[d.Name][unitPath] = true
			}
		}
	}

	out := make(map[string][]string, len(declaredBy))
	for input, set := range declaredBy {
		for unitPath := range set {
			out[input] = append(out[input], unitPath)
		}
		sort.Strings(out[input])
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
