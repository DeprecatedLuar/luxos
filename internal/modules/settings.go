package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// OptionsFile in a folder unit declares what each host may set for it.
const OptionsFile = "options.nix"

// optionsTop is the only top-level name an options file may set.
const optionsTop = "options"

const attrSep = "."

// SettingsState is how a unit's settings stand.
type SettingsState string

const (
	SettingsNone   SettingsState = "none" // no options.nix
	SettingsOK     SettingsState = "ok"
	SettingsBroken SettingsState = "broken" // options.nix or the host's settings file breaks a rule
)

// UnitSettings is one configurable unit for a host: its options.nix, the
// host's settings file for it (existing or not), the declared keys with
// their defaults, and every rule either file breaks.
type UnitSettings struct {
	Module   Module
	Options  string
	File     string
	Declared []nix.Setting
	Problems []string
}

// Namespace is the attribute path a unit's options live under: its name split at "/".
func Namespace(name string) []string {
	return strings.Split(name, nameSep)
}

// optionsOf returns m's options.nix when m is a folder unit holding one.
func optionsOf(m Module) (string, bool) {
	path := filepath.Join(m.Abs, OptionsFile)
	return path, isFile(path)
}

// Settings checks every unit of ms that has an options.nix, and h's settings
// file for it, against the settings rules. Units without one are left out.
func (h *Host) Settings(ms []Module) ([]UnitSettings, error) {
	var out []UnitSettings
	var files []string
	for _, m := range ms {
		opts, ok := optionsOf(m)
		if !ok {
			continue
		}
		out = append(out, UnitSettings{Module: m, Options: opts, File: config.SettingsPath(h.HostDir, m.Name)})
		files = append(files, opts)
	}
	if len(out) == 0 {
		return nil, nil
	}
	read, broken, err := nix.ReadOptions(files...)
	if err != nil {
		return nil, err
	}
	for i := range out {
		u := &out[i]
		abs, err := filepath.Abs(u.Options)
		if err != nil {
			return nil, err
		}
		if msg, bad := broken[abs]; bad {
			u.Problems = append(u.Problems, fmt.Sprintf("%s: %s (options.nix takes only lib, and every default is a plain value)", u.Options, msg))
		} else {
			u.Declared, u.Problems = checkOptions(u.Options, Namespace(u.Module.Name), read[abs])
		}
		problem, err := importsOptions(u.Module)
		if err != nil {
			return nil, err
		}
		if problem != "" {
			u.Problems = append(u.Problems, problem)
		}
		if _, err := os.Stat(u.File); err == nil {
			if _, err := nix.Parse(u.File); err != nil {
				u.Problems = append(u.Problems, fmt.Sprintf("%s does not parse: %v", u.File, err))
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return out, nil
}

// checkOptions applies the options.nix rules to what file declares under ns.
func checkOptions(file string, ns []string, f nix.OptionsFile) ([]nix.Setting, []string) {
	var declared []nix.Setting
	var problems []string
	for _, name := range f.Top {
		if name != optionsTop {
			problems = append(problems, fmt.Sprintf("%s: sets %s; options.nix only declares options", file, name))
		}
	}
	want := strings.Join(ns, attrSep)
	for _, leaf := range f.Options {
		path := strings.Join(leaf.Path, attrSep)
		switch {
		case len(leaf.Path) != len(ns)+1 || strings.Join(leaf.Path[:len(ns)], attrSep) != want:
			problems = append(problems, fmt.Sprintf("%s: options.%s must be options.%s.<key>", file, path, want))
		case !leaf.Option:
			problems = append(problems, fmt.Sprintf("%s: options.%s is not a lib.mkOption", file, path))
		case !leaf.HasType:
			problems = append(problems, fmt.Sprintf("%s: options.%s has no type", file, path))
		case !leaf.HasDefault:
			problems = append(problems, fmt.Sprintf("%s: options.%s has no default", file, path))
		case !leaf.PlainDefault:
			problems = append(problems, fmt.Sprintf("%s: options.%s default is not a plain value (null, bool, number, string, or a list or attrset of those)", file, path))
		default:
			declared = append(declared, nix.Setting{Path: leaf.Path, Value: leaf.Default, Comment: leaf.Description})
		}
	}
	return declared, problems
}

// importsOptions reports a problem when m's default.nix does not import ./options.nix.
func importsOptions(m Module) (string, error) {
	entry := filepath.Join(m.Abs, entrypointName)
	paths, err := nix.Paths(entry)
	if err != nil {
		return fmt.Sprintf("%s does not parse: %v", entry, err), nil
	}
	want := filepath.Join(filepath.Dir(entry), OptionsFile)
	for _, p := range paths {
		if p == want {
			return "", nil
		}
	}
	return fmt.Sprintf("%s does not import ./%s", entry, OptionsFile), nil
}

// selected returns the units h's selection names, in selection order; a line
// naming no unit is skipped (heal reports it).
func (h *Host) selected() []Module {
	var out []Module
	for _, line := range h.Selection {
		if m, ok := h.Find(NameFromPath(line)); ok {
			out = append(out, m)
		}
	}
	return out
}

// SettingsChange is one settings file and the keys a sync wrote or commented out in it.
type SettingsChange struct {
	File string
	Keys []string
}

// SettingsReport is what SyncSettings did, and the units it skipped for
// breaking a rule (their Problems say why).
type SettingsReport struct {
	Created   []string
	Appended  []SettingsChange
	Commented []SettingsChange
	Broken    []UnitSettings
}

// SyncSettings makes h's settings file of every selected unit with an
// options.nix hold every declared key: a missing file is created with the
// defaults, a missing key is appended with its default, and an undeclared
// assignment is commented out. A unit breaking a rule, or whose file cannot
// be read or written, is skipped and returned in Broken.
func SyncSettings(h *Host) (SettingsReport, error) {
	return syncSettings(h, h.selected())
}

// SyncUnitSettings syncs h's settings file of m, selected or not, and returns its path.
func SyncUnitSettings(h *Host, m Module) (string, SettingsReport, error) {
	if _, ok := optionsOf(m); !ok {
		return "", SettingsReport{}, fmt.Errorf("module '%s' has no settings", m.Name)
	}
	rep, err := syncSettings(h, []Module{m})
	return config.SettingsPath(h.HostDir, m.Name), rep, err
}

func syncSettings(h *Host, ms []Module) (SettingsReport, error) {
	var rep SettingsReport
	us, err := h.Settings(ms)
	if err != nil {
		return rep, err
	}
	var existing []UnitSettings
	for _, u := range us {
		if len(u.Problems) > 0 {
			rep.Broken = append(rep.Broken, u)
			continue
		}
		_, err := os.Stat(u.File)
		switch {
		case os.IsNotExist(err):
			if err := createSettings(u); err != nil {
				u.Problems = append(u.Problems, err.Error())
				rep.Broken = append(rep.Broken, u)
				continue
			}
			rep.Created = append(rep.Created, u.File)
		case err != nil:
			return rep, err
		default:
			existing = append(existing, u)
		}
	}
	if len(existing) == 0 {
		return rep, nil
	}

	reqs := make([]nix.SettingsRequest, len(existing))
	for i, u := range existing {
		reqs[i] = nix.SettingsRequest{File: u.File, Depth: len(Namespace(u.Module.Name)) + 1}
	}
	read, broken, err := nix.ReadSettings(reqs)
	if err != nil {
		return rep, err
	}
	for i, u := range existing {
		abs, err := filepath.Abs(u.File)
		if err != nil {
			return rep, err
		}
		if msg, bad := broken[abs]; bad {
			u.Problems = append(u.Problems, fmt.Sprintf("%s: %s", u.File, msg))
			rep.Broken = append(rep.Broken, u)
			continue
		}
		missing, drop := diffSettings(u.Declared, read[abs])
		comments := settingComments(u.Declared)
		if err := nix.CommentSettingLines(u.File, declaredLeaves(comments, read[abs]), comments); err != nil {
			u.Problems = append(u.Problems, err.Error())
			rep.Broken = append(rep.Broken, u)
			continue
		}
		if len(drop) > 0 {
			if err := nix.CommentSettings(u.File, reqs[i].Depth, drop); err != nil {
				u.Problems = append(u.Problems, err.Error())
				rep.Broken = append(rep.Broken, u)
				continue
			}
			rep.Commented = append(rep.Commented, SettingsChange{File: u.File, Keys: joinPaths(drop)})
		}
		if len(missing) > 0 {
			if err := nix.AppendSettings(u.File, missing); err != nil {
				u.Problems = append(u.Problems, err.Error())
				rep.Broken = append(rep.Broken, u)
				continue
			}
			paths := make([][]string, len(missing))
			for j, s := range missing {
				paths[j] = s.Path
			}
			rep.Appended = append(rep.Appended, SettingsChange{File: u.File, Keys: joinPaths(paths)})
		}
	}
	return rep, nil
}

func createSettings(u UnitSettings) error {
	if err := config.MkdirAll(filepath.Dir(u.File)); err != nil {
		return err
	}
	_, err := config.CreateFile(u.File, nix.RenderSettings(u.Declared))
	return err
}

// diffSettings returns the declared settings a file lacks and the file's
// assignments that are not declared.
func diffSettings(declared []nix.Setting, have []nix.SettingLeaf) (missing []nix.Setting, drop [][]string) {
	declaredKeys := map[string]bool{}
	for _, s := range declared {
		declaredKeys[strings.Join(s.Path, attrSep)] = true
	}
	haveKeys := map[string]bool{}
	for _, l := range have {
		key := strings.Join(l.Path, attrSep)
		haveKeys[key] = true
		if !declaredKeys[key] {
			drop = append(drop, l.Path)
		}
	}
	for _, s := range declared {
		if !haveKeys[strings.Join(s.Path, attrSep)] {
			missing = append(missing, s)
		}
	}
	return missing, drop
}

// declaredLeaves is the file's assignments whose key is in comments.
func declaredLeaves(comments map[string]string, have []nix.SettingLeaf) []nix.SettingLeaf {
	var out []nix.SettingLeaf
	for _, l := range have {
		if _, ok := comments[strings.Join(l.Path, attrSep)]; ok {
			out = append(out, l)
		}
	}
	return out
}

// settingComments maps each declared key to the comment its line carries.
func settingComments(declared []nix.Setting) map[string]string {
	out := map[string]string{}
	for _, s := range declared {
		out[strings.Join(s.Path, attrSep)] = s.Comment
	}
	return out
}

func joinPaths(paths [][]string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = strings.Join(p, attrSep)
	}
	return out
}

// MoveSettings renames unit name's settings file, and the folder of its
// submodules' settings when it is a bundle, to newName in every host in
// scope (host and skipHosts as for RetargetSelections); newName "" deletes
// them. A missing entry is skipped. An entry whose target already exists is
// left in place and returned in kept.
func MoveSettings(machinesDir, name, newName, host string, skipHosts []string) (kept []string, err error) {
	entrypoints, err := scopedEntrypoints(machinesDir, host, skipHosts)
	if err != nil {
		return nil, err
	}
	for _, ep := range entrypoints {
		hostDir := filepath.Dir(ep)
		pairs := [][2]string{
			{config.SettingsPath(hostDir, name), ""},
			{config.SettingsFolder(hostDir, name), ""},
		}
		if newName != "" {
			pairs[0][1] = config.SettingsPath(hostDir, newName)
			pairs[1][1] = config.SettingsFolder(hostDir, newName)
		}
		for _, pair := range pairs {
			from, to := pair[0], pair[1]
			if _, err := os.Lstat(from); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return kept, err
			}
			if to == "" {
				if err := os.RemoveAll(from); err != nil {
					return kept, err
				}
				continue
			}
			if _, err := os.Lstat(to); err == nil {
				kept = append(kept, from)
				continue
			} else if !os.IsNotExist(err) {
				return kept, err
			}
			if err := config.MkdirAll(filepath.Dir(to)); err != nil {
				return kept, err
			}
			if err := os.Rename(from, to); err != nil {
				return kept, err
			}
		}
	}
	return kept, nil
}

// StagedSettings returns the names of h's selected units that have an
// options.nix and a settings file, in selection order.
func (h *Host) StagedSettings() []string {
	var out []string
	for _, m := range h.selected() {
		if _, ok := optionsOf(m); !ok {
			continue
		}
		if isFile(config.SettingsPath(h.HostDir, m.Name)) {
			out = append(out, m.Name)
		}
	}
	return out
}
