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
			declared = append(declared, nix.Setting{Path: leaf.Path, Value: leaf.Default})
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
