// Package units walks CONFIG_DIR/modules to discover named units, resolves
// a name to its path, and derives a unit's name from an import path.
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

// Walk never descends into it when walking modulesDir; the active host's
// local units are discovered separately, via localModulesDir, and prefixed "local/".
const localName = "local"

const localPrefix = "local/"

// bundleModulesDir is the subdirectory of a folder unit that holds its
// submodules; the unit is then a bundle.
const bundleModulesDir = "modules"

const nameSep = "/"

// Path is relative to that directory, with no leading "./".
// A submodule's Name is qualified by its bundle ("eduardo/git").
// Shadows is the modules-relative path of the shared unit a local unit
// hides, or, for a submodule of a shadowing bundle, the path it takes when
// the bundle is staged in the shared bundle's place; "" otherwise.
type Unit struct {
	Name    string
	Path    string
	Shadows string
}

// Walk discovers every unit for a host: the shared units under modulesDir
// plus, when localModulesDir is given, that host's own local units (L6).
// Over modulesDir, per §3 Walk:
//   - the root's own default.nix is skipped;
//   - the root's own "local" entry (the reserved link to the active host's
//     local modules) is skipped, so it is never descended into here;
//   - a *.nix file is a unit;
//   - a directory with its own default.nix is a unit, not descended into;
//   - a directory without one is a transparent category and is descended
//     into;
//   - symlinks are followed (os.Stat, not Lstat).
//
// localModulesDir is walked the same way (its own root "default.nix"/"local"
// entries have no special meaning there), and every unit found under it has
// its Path prefixed "local/". A missing localModulesDir yields no local
// units; an empty localModulesDir ("") skips the local walk entirely.
//
// The result is sorted by Name (byte order). A name claimed by more than one
// shared unit, or by more than one local unit, is a hard error listing every
// path that claims it. A local unit whose name a shared unit also claims
// shadows it: it gets Shadows set to the shared path and the shared unit is
// dropped from the result.
func Walk(modulesDir, localModulesDir string) ([]Unit, error) {
	var shared []Unit
	if err := walkDir(modulesDir, modulesDir, "", true, &shared); err != nil {
		return nil, err
	}

	var local []Unit
	if localModulesDir != "" {
		if _, err := os.Stat(localModulesDir); err == nil {
			var localRaw []Unit
			if err := walkDir(localModulesDir, localModulesDir, "", false, &localRaw); err != nil {
				return nil, err
			}
			for _, u := range localRaw {
				local = append(local, Unit{Name: u.Name, Path: localPrefix + u.Path})
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if err := checkDuplicates(shared); err != nil {
		return nil, err
	}
	if err := checkDuplicates(local); err != nil {
		return nil, err
	}

	sharedByName := make(map[string]Unit, len(shared))
	for _, u := range shared {
		sharedByName[u.Name] = u
	}
	shadowed := make(map[string]bool)
	for _, l := range local {
		if Parent(l.Name) != "" {
			continue
		}
		orig, ok := sharedByName[l.Name]
		if !ok {
			continue
		}
		shadowed[l.Name] = true
		for i, u := range local {
			switch {
			case u.Name == l.Name:
				local[i].Shadows = orig.Path
			case Top(u.Name) == l.Name:
				local[i].Shadows = filepath.Join(filepath.Dir(orig.Path), filepath.Base(l.Path), strings.TrimPrefix(u.Path, l.Path))
			}
		}
	}

	raw := make([]Unit, 0, len(shared)+len(local))
	for _, u := range shared {
		if !shadowed[Top(u.Name)] {
			raw = append(raw, u)
		}
	}
	raw = append(raw, local...)
	sort.Slice(raw, func(i, j int) bool { return raw[i].Name < raw[j].Name })
	return raw, nil
}

func checkDuplicates(us []Unit) error {
	claimants := make(map[string][]string, len(us))
	for _, u := range us {
		claimants[u.Name] = append(claimants[u.Name], u.Path)
	}

	var dupeNames []string
	for name, paths := range claimants {
		if len(paths) > 1 {
			dupeNames = append(dupeNames, name)
		}
	}
	if len(dupeNames) == 0 {
		return nil
	}
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
	return fmt.Errorf("%s", b.String())
}

// walkDir emits (unsorted, duplicates left in) units found under dir into
// *out. root is the fixed walk root, used to compute relative paths.
// prefix is the qualified-name prefix of the bundle being walked ("" outside
// one). skipLocalRoot, when true, also skips an entry named localName at the
// walk root (used for the shared modulesDir walk only; L5).
func walkDir(root, dir, prefix string, skipLocalRoot bool, out *[]Unit) error {
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
		if dir == root && skipLocalRoot && entry.Name() == localName {
			continue
		}

		if info.IsDir() {
			defaultNix := filepath.Join(entryPath, entrypointName)
			relPath, err := filepath.Rel(root, entryPath)
			if err != nil {
				return err
			}
			if fi, err := os.Stat(defaultNix); err == nil && !fi.IsDir() {
				name := prefix + entry.Name()
				*out = append(*out, Unit{Name: name, Path: relPath})
				if IsBundle(entryPath) {
					if err := walkBundle(root, entryPath, relPath, name, skipLocalRoot, out); err != nil {
						return err
					}
				}
			} else {
				if entry.Name() == bundleModulesDir {
					return fmt.Errorf("%s: a folder named modules is only allowed directly inside a module (a bundle)", relPath)
				}
				if err := walkDir(root, entryPath, prefix, skipLocalRoot, out); err != nil {
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
			name := prefix + strings.TrimSuffix(entry.Name(), ".nix")
			*out = append(*out, Unit{Name: name, Path: relPath})
		}
	}

	return nil
}

// walkBundle emits the submodules under a bundle's modules/ folder, named
// under the bundle's qualified name.
func walkBundle(root, bundleDir, rel, name string, skipLocalRoot bool, out *[]Unit) error {
	modulesDir := filepath.Join(bundleDir, bundleModulesDir)
	if fi, err := os.Stat(filepath.Join(modulesDir, entrypointName)); err == nil && !fi.IsDir() {
		return fmt.Errorf("%s/%s has its own default.nix: a bundle's modules/ folder is not a module", rel, bundleModulesDir)
	}
	return walkDir(root, modulesDir, name+nameSep, skipLocalRoot, out)
}

// IsBundle reports whether dir is a folder unit with a modules/ subdirectory.
func IsBundle(dir string) bool {
	if fi, err := os.Stat(filepath.Join(dir, entrypointName)); err != nil || fi.IsDir() {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, bundleModulesDir))
	return err == nil && fi.IsDir()
}

// Parent is the qualified name of a submodule's bundle, "" for a top-level name.
func Parent(name string) string {
	i := strings.LastIndex(name, nameSep)
	if i < 0 {
		return ""
	}
	return name[:i]
}

// Top is the outermost bundle of a qualified name, or the name itself.
func Top(name string) string {
	top, _, _ := strings.Cut(name, nameSep)
	return top
}

func Find(us []Unit, name string) (Unit, bool) {
	for _, u := range us {
		if u.Name == name {
			return u, true
		}
	}
	return Unit{}, false
}

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

// NameFromPath derives a unit's name from an import path: strip a trailing
// "/default.nix", else a trailing ".nix", then take the basename. Every
// "modules" segment (not the first or last) makes the segment before it a
// bundle, and the name is qualified by those bundles ("eduardo/git").
func NameFromPath(path string) string {
	trimmed := strings.TrimSuffix(path, ".nix")
	if t := strings.TrimSuffix(path, nameSep+entrypointName); t != path {
		trimmed = t
	}
	segs := strings.Split(strings.TrimRight(trimmed, nameSep), nameSep)
	last := len(segs) - 1
	var parts []string
	for i := 1; i < last; i++ {
		if segs[i] == bundleModulesDir {
			parts = append(parts, segs[i-1])
		}
	}
	return strings.Join(append(parts, segs[last]), nameSep)
}
