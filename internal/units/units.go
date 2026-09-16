// Package units walks CONFIG_DIR/modules to discover named units, resolves
// a name to its path, and derives a unit's name from an import path — per
// implementation-plan.md §3 "Name from an import path" and "Walk".
package units

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// entrypointName is the file name that turns a directory into a unit (and
// that the walk root's own copy is skipped for).
const entrypointName = "default.nix"

// Unit is one named module found under a modules directory. Path is
// relative to that directory, with no leading "./".
type Unit struct {
	Name string
	Path string
}

// Walk discovers every unit under modulesDir per §3 Walk:
//   - the root's own default.nix is skipped;
//   - a *.nix file is a unit;
//   - a directory with its own default.nix is a unit, not descended into;
//   - a directory without one is a transparent category and is descended
//     into;
//   - symlinks are followed (os.Stat, not Lstat).
//
// The result is sorted by Name (byte order). A name claimed by more than
// one unit is a hard error listing every path that claims it.
func Walk(modulesDir string) ([]Unit, error) {
	var raw []Unit
	if err := walkDir(modulesDir, modulesDir, &raw); err != nil {
		return nil, err
	}

	sort.Slice(raw, func(i, j int) bool { return raw[i].Name < raw[j].Name })

	claimants := make(map[string][]string, len(raw))
	for _, u := range raw {
		claimants[u.Name] = append(claimants[u.Name], u.Path)
	}

	var dupeNames []string
	for name, paths := range claimants {
		if len(paths) > 1 {
			dupeNames = append(dupeNames, name)
		}
	}
	if len(dupeNames) > 0 {
		sort.Strings(dupeNames)
		var b strings.Builder
		for i, name := range dupeNames {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "duplicate module name '%s':", name)
			paths := append([]string(nil), claimants[name]...)
			sort.Strings(paths)
			for _, p := range paths {
				fmt.Fprintf(&b, "\n  - %s", p)
			}
		}
		return nil, fmt.Errorf("%s", b.String())
	}

	return raw, nil
}

// walkDir emits (unsorted, duplicates left in) units found under dir into
// *out. root is the fixed walk root, used to compute relative paths.
func walkDir(root, dir string, out *[]Unit) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())

		// os.Stat follows symlinks, matching bash's "-d"/"-f" tests.
		info, err := os.Stat(entryPath)
		if err != nil {
			// A broken symlink or a file that vanished mid-walk: skip it,
			// matching bash's "[[ -e ]] || continue".
			continue
		}

		if dir == root && entry.Name() == entrypointName {
			continue
		}

		if info.IsDir() {
			defaultNix := filepath.Join(entryPath, entrypointName)
			if fi, err := os.Stat(defaultNix); err == nil && !fi.IsDir() {
				relPath, err := filepath.Rel(root, entryPath)
				if err != nil {
					return err
				}
				*out = append(*out, Unit{Name: entry.Name(), Path: relPath})
			} else {
				if err := walkDir(root, entryPath, out); err != nil {
					return err
				}
			}
			continue
		}

		if strings.HasSuffix(entry.Name(), ".nix") {
			relPath, err := filepath.Rel(root, entryPath)
			if err != nil {
				return err
			}
			name := strings.TrimSuffix(entry.Name(), ".nix")
			*out = append(*out, Unit{Name: name, Path: relPath})
		}
	}

	return nil
}

// Resolve looks up name in units, returning its path and true when found.
// Callers are expected to have gone through Walk first, which already
// guarantees uniqueness of Name across units.
func Resolve(units []Unit, name string) (string, bool) {
	for _, u := range units {
		if u.Name == name {
			return u.Path, true
		}
	}
	return "", false
}

// NameFromPath derives a unit's name from an import path, per §3 "Name
// from an import path": strip a trailing "/default.nix", else a trailing
// ".nix", then take the basename.
func NameFromPath(path string) string {
	if trimmed := strings.TrimSuffix(path, "/"+entrypointName); trimmed != path {
		return filepath.Base(trimmed)
	}
	trimmed := strings.TrimSuffix(path, ".nix")
	return filepath.Base(trimmed)
}
