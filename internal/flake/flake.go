// Package flake derives the status of a host's flake inputs: where each is
// declared, whether upstream has moved past the locked revision, and which
// version it is on. It asks upstream through an Upstream and never prints.
package flake

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

const (
	sharedDisplayDir   = "modules"
	localDisplayDir    = "local/modules"
	machinesDisplayDir = ".local/machines"

	pathSep     = "/"
	shortRevLen = 7
)

// Builtin inputs are declared by luxos when it renders flake.nix, so they are
// declared even when no module says so, and always sit at the tree root.
var Builtin = []string{"luxos", "nixpkgs"}

// Note marks a locked node against upstream.
type Note string

const (
	NoteBehind  Note = "↑" // upstream's tip differs from the locked revision
	NoteUnknown Note = "?" // the check failed
)

// Status is how a flake input stands against upstream.
type Status string

const (
	StatusBehind  Status = "behind"
	StatusCurrent Status = "current"
	StatusUnknown Status = "unknown"
)

// StatusOf is unknown when offline, not locked, or the check failed; behind
// or current otherwise.
func StatusOf(locked, online bool, note Note) Status {
	switch {
	case !locked || !online || note == NoteUnknown:
		return StatusUnknown
	case note == NoteBehind:
		return StatusBehind
	default:
		return StatusCurrent
	}
}

// Decl is one place an input is declared. Unit is empty for the host's
// machine.nix.
type Decl struct {
	Unit string
	File string // display path, e.g. "modules/apps/shell.nix"
	Line int
	URL  string
}

// Declarations returns every luxos.inputs declaration of the modules h
// builds and of its machine.nix, by input name, sorted by file then line.
func Declarations(h *modules.Host) (map[string][]Decl, error) {
	built, err := h.Built()
	if err != nil {
		return nil, err
	}

	out := map[string][]Decl{}
	for _, m := range built {
		display := sharedDisplayDir
		if strings.HasPrefix(m.Path, modules.LocalPrefix) {
			display = localDisplayDir
		}
		files, err := m.Files()
		if err != nil {
			return nil, err
		}
		decls, err := nix.InputDecls(files...)
		if err != nil {
			return nil, err
		}
		for _, d := range decls {
			rel, err := filepath.Rel(m.Root(), d.File)
			if err != nil {
				return nil, err
			}
			out[d.Name] = append(out[d.Name], Decl{
				Unit: m.Path,
				File: filepath.ToSlash(filepath.Join(display, rel)),
				Line: d.Line,
				URL:  d.URL,
			})
		}
	}

	base, err := baseChannelDecl(h.HostDir)
	if err != nil {
		return nil, err
	}
	if base != nil {
		out[nix.BaseChannelInput] = append(out[nix.BaseChannelInput], *base)
	}
	for _, decls := range out {
		sort.Slice(decls, func(i, j int) bool {
			if decls[i].File != decls[j].File {
				return decls[i].File < decls[j].File
			}
			return decls[i].Line < decls[j].Line
		})
	}
	return out, nil
}

// baseChannelDecl is nil when the host's machine.nix has no base channel (the
// rebuild preflight reports that). It belongs to no unit.
func baseChannelDecl(hostDir string) (*Decl, error) {
	url, line, err := nix.BaseChannel(filepath.Join(hostDir, config.MachineFile))
	if errors.Is(err, nix.ErrNoBaseChannel) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rel := filepath.Join(machinesDisplayDir, filepath.Base(hostDir), config.MachineFile)
	return &Decl{File: filepath.ToSlash(rel), Line: line, URL: url}, nil
}

// Units returns, by input name, the sorted units that declare it.
func Units(decls map[string][]Decl) map[string][]string {
	out := make(map[string][]string, len(decls))
	for input, ds := range decls {
		set := map[string]bool{}
		for _, d := range ds {
			if d.Unit != "" && !set[d.Unit] {
				set[d.Unit] = true
				out[input] = append(out[input], d.Unit)
			}
		}
		sort.Strings(out[input])
	}
	return out
}

// check asks up for node's tip and compares it with the locked revision.
func check(up Upstream, node nix.LockNode) (tip string, note Note, err error) {
	tip, err = up.Tip(node.Original)
	switch {
	case err != nil:
		return "", NoteUnknown, err
	case tip != node.Locked.Rev:
		return tip, NoteBehind, nil
	}
	return tip, "", nil
}

// Notes checks the upstream tip of every locked node reachable from the root
// one goroutine per node. It returns node key -> note:
// NoteBehind when the tip differs from the locked rev, NoteUnknown on any
// failure, absent when equal.
func Notes(up Upstream, lock nix.Lock) map[string]Note {
	keys := map[string]bool{}
	var walk func(key string)
	walk = func(key string) {
		if keys[key] {
			return
		}
		keys[key] = true
		for _, target := range lock.Nodes[key].Inputs {
			walk(target)
		}
	}
	for _, key := range lock.Nodes[nix.LockRootNode].Inputs {
		walk(key)
	}
	delete(keys, nix.LockRootNode)

	notes := map[string]Note{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for key := range keys {
		node := lock.Nodes[key]
		if node.Locked.Rev == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, note, _ := check(up, node); note != "" {
				mu.Lock()
				notes[key] = note
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return notes
}

// VersionInfo is what `flake <name>` shows about a locked node.
type VersionInfo struct {
	Current  string // tag on the locked rev, else its short rev
	Latest   string // tag nearest the tip, else its short rev; "?" when unknown; "" when up to date
	Ahead    int    // commits from the locked rev to the tip
	HasAhead bool   // Ahead is known (compare succeeded)
	Checked  bool   // upstream was asked
	Note     Note
}

// Version reports node's version against up. A nil up (offline), or a node
// without a locked rev, gives the current rev only.
func Version(up Upstream, node nix.LockNode) VersionInfo {
	rev := node.Locked.Rev
	if up == nil || rev == "" {
		return VersionInfo{Current: shortRev(rev)}
	}

	tip, note, tipErr := check(up, node)
	tags, tagsErr := up.Tags(node.Original)

	info := VersionInfo{Current: shortRev(rev), Checked: true, Note: note}
	if tag, ok := tags[rev]; ok {
		info.Current = tag
	}
	switch {
	case tipErr != nil:
		info.Latest = string(NoteUnknown)
		return info
	case tip == rev:
		return info
	}

	ahead, shas, cmpErr := up.Compare(node.Original, rev, tip)
	switch {
	case errors.Is(cmpErr, ErrUnsupported):
		// No range to search: an exact tag on the tip or the tip itself.
		info.Latest = shortRev(tip)
		if tag, ok := tags[tip]; ok {
			info.Latest = tag
		}
	case cmpErr != nil || (tagsErr != nil && !errors.Is(tagsErr, ErrUnsupported)):
		info.Latest = string(NoteUnknown)
	default:
		info.Latest = shortRev(tip)
		for i := len(shas) - 1; i >= 0; i-- {
			if tag, ok := tags[shas[i]]; ok {
				info.Latest = tag
				break
			}
		}
	}
	if cmpErr == nil {
		info.Ahead, info.HasAhead = ahead, true
	}
	return info
}

// NestedPaths returns, sorted by traversal, every path "a/b/name" under which
// the lock pulls in an input called name below a root input.
func NestedPaths(lock nix.Lock, name string) []string {
	var found []string
	var walk func(key, prefix string, ancestors map[string]bool)
	walk = func(key, prefix string, ancestors map[string]bool) {
		inputs := lock.Nodes[key].Inputs
		for _, child := range sortedKeys(inputs) {
			target := inputs[child]
			if ancestors[target] {
				continue
			}
			path := child
			if prefix != "" {
				path = prefix + pathSep + child
				if child == name {
					found = append(found, path)
				}
			}
			ancestors[target] = true
			walk(target, path, ancestors)
			delete(ancestors, target)
		}
	}
	walk(nix.LockRootNode, "", map[string]bool{})
	return found
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func shortRev(rev string) string {
	if len(rev) > shortRevLen {
		return rev[:shortRevLen]
	}
	return rev
}
