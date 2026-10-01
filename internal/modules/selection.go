package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

// bundleModulesInfix separates a bundle's path from its submodules' in an
// import path.
const bundleModulesInfix = "/modules/"

// New == "" means the line was removed.
type Change struct {
	File, Old, New string
}

// ReadSelection returns the import paths of file, without "./". It is a hard
// error if file doesn't have exactly one recognizable imports block.
func ReadSelection(file string) ([]string, error) {
	paths, ok, err := nix.ListImports(file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, shapeError(file)
	}
	return paths, nil
}

// Importers returns every entrypoint file with a line whose name (from its
// path, per NameFromPath) matches name — stale paths included. Read-only.
// host == "" scans every <machinesDir>/*/modules.nix; otherwise only
// <machinesDir>/<host>/modules.nix is considered. A host whose file isn't
// recognizable is silently skipped (same tolerance as Retarget/Heal — it
// just can't be a hit).
func Importers(machinesDir, name, host string) ([]string, error) {
	files, err := entrypointsFor(machinesDir, host)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, file := range files {
		paths, ok, err := nix.ListImports(file)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, p := range paths {
			if NameFromPath(p) == name {
				out = append(out, file)
				break
			}
		}
	}
	return out, nil
}

// RetargetSelections is the writer of host selections: rewrite every import
// line whose name matches name to newPath, or delete it when newPath is
// "". host == "" reaches every <machinesDir>/*/modules.nix (the cross-host
// case, for a shared unit); otherwise only <machinesDir>/<host>/modules.nix is
// touched (the local-unit case, L7). Returns every change made; a no-op
// line (already at newPath) is left untouched and unreported, so a second
// run returns no changes. A host whose file isn't recognizable is warned
// about and left untouched. The entrypoints of skipHosts are never rewritten.
func RetargetSelections(machinesDir, name, newPath, host string, skipHosts []string) ([]Change, error) {
	changes, _, err := retarget(machinesDir, name, newPath, host, skipHosts)
	return changes, err
}

func retarget(machinesDir, name, newPath, host string, skipHosts []string) ([]Change, []string, error) {
	files, err := scopedEntrypoints(machinesDir, host, skipHosts)
	if err != nil {
		return nil, nil, err
	}

	match := func(p string) bool { return NameFromPath(p) == name }
	newPath = strings.TrimPrefix(newPath, "./")

	var changes []Change
	var warnings []string

	for _, file := range files {
		old, ok, err := nix.RetargetImports(file, match, newPath)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s does not have exactly one recognizable imports block, skipping", file))
			continue
		}
		for _, o := range old {
			changes = append(changes, Change{File: file, Old: o, New: newPath})
		}
	}

	return changes, warnings, nil
}

// RetargetPrefix rewrites every import line that selects a submodule of the
// bundle at oldPrefix (a path starting with oldPrefix + "/modules/") so the
// bundle prefix becomes newPrefix, or deletes the line when newPrefix is "".
// Host scope and skipHosts are those of Retarget; a host whose file isn't
// recognizable is left untouched.
func RetargetPrefix(machinesDir, oldPrefix, newPrefix, host string, skipHosts []string) ([]Change, error) {
	files, err := scopedEntrypoints(machinesDir, host, skipHosts)
	if err != nil {
		return nil, err
	}

	var changes []Change
	for _, file := range files {
		paths, ok, err := nix.ListImports(file)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		done := make(map[string]bool)
		for _, path := range paths {
			if !strings.HasPrefix(path, oldPrefix+bundleModulesInfix) || done[path] {
				continue
			}
			done[path] = true
			newPath := ""
			if newPrefix != "" {
				newPath = newPrefix + strings.TrimPrefix(path, oldPrefix)
			}
			old, _, err := nix.RetargetImports(file, func(p string) bool { return p == path }, newPath)
			if err != nil {
				return nil, err
			}
			for _, o := range old {
				changes = append(changes, Change{File: file, Old: o, New: newPath})
			}
		}
	}
	return changes, nil
}

// Heal rewrites every broken import line in every host's entrypoint,
// per host (L7): each host's selection lines are checked and resolved
// against that host's own unit set (shared units plus that host's own
// local/ units, per Walk(modulesDir, <machinesDir>/<host>/modules)).
//   - path exists (local/ prefixed against that host's local modules dir,
//     otherwise against modulesDir): keep.
//   - name resolves to a local/ unit: retarget scoped to this host only.
//   - name resolves to a shared unit: retarget in every host referencing
//     it (once per name, as before).
//   - name resolves to nothing, active host: error (collecting every such
//     line into one), or with prune: remove and report as a Change.
//   - name resolves to nothing, other host: warning, untouched.
//   - path is config.HardwareUnitPath on a non-active host: skipped, since
//     the hardware link exists only in the active host's modules.
//
// A host whose own Walk fails errors the whole call when it's
// activeHost, otherwise adds a warning ("<host>: <err>") and skips that
// host entirely. Heal is idempotent.
func Heal(machinesDir, modulesDir, activeHost string, prune bool) ([]Change, []string, error) {
	files, err := hostEntrypoints(machinesDir)
	if err != nil {
		return nil, nil, err
	}

	var changes []Change
	var warnings []string
	var unresolved []string // "file: ./path" for active-host unresolved names

	// Track which (host, name) local retargets and which shared-name
	// retargets have already been applied, so a name referenced by more
	// than one broken line in the same run is only retargeted once —
	// Retarget itself loops every relevant host already.
	handledLocal := make(map[string]bool)
	handledShared := make(map[string]bool)

	for _, file := range files {
		host := filepath.Base(filepath.Dir(file))

		us, err := Walk(modulesDir, filepath.Join(machinesDir, host, localModulesDirName))
		if err != nil {
			if host == activeHost {
				return nil, nil, err
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s", host, err))
			continue
		}

		paths, ok, err := nix.ListImports(file)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("%s does not have exactly one recognizable imports block, skipping heal", file))
			continue
		}

		isActive := host == activeHost

		for _, itPath := range paths {
			if !isActive && itPath == config.HardwareUnitPath {
				continue
			}
			var fullPath string
			if rest, cut := strings.CutPrefix(itPath, LocalPrefix); cut {
				fullPath = filepath.Join(machinesDir, host, localModulesDirName, rest)
			} else {
				fullPath = filepath.Join(modulesDir, itPath)
			}
			if _, err := os.Stat(fullPath); err == nil {
				continue
			}

			name := NameFromPath(itPath)
			if found, ok := Find(us, name); ok {
				resolved := found.Path
				if strings.HasPrefix(resolved, LocalPrefix) {
					key := host + "\x00" + name
					if handledLocal[key] {
						continue
					}
					handledLocal[key] = true
					rc, rw, err := retarget(machinesDir, name, resolved, host, nil)
					if err != nil {
						return nil, nil, err
					}
					changes = append(changes, rc...)
					warnings = append(warnings, rw...)
				} else {
					if handledShared[name] {
						continue
					}
					handledShared[name] = true
					rc, rw, err := retarget(machinesDir, name, resolved, "", nil)
					if err != nil {
						return nil, nil, err
					}
					changes = append(changes, rc...)
					warnings = append(warnings, rw...)
				}
				continue
			}

			if isActive {
				if prune {
					if err := nix.RemoveImport(file, itPath); err != nil {
						return nil, nil, err
					}
					changes = append(changes, Change{File: file, Old: itPath, New: ""})
				} else {
					unresolved = append(unresolved, fmt.Sprintf("%s: ./%s does not exist and '%s' does not resolve to any module", file, itPath, name))
				}
			} else {
				warnings = append(warnings, fmt.Sprintf("%s: ./%s does not exist and '%s' does not resolve to any module — left untouched", file, itPath, name))
			}
		}
	}

	if len(unresolved) > 0 {
		msg := strings.Join(unresolved, "\n") + "\n  Fix the import by hand, or rerun with --prune to remove it."
		return changes, warnings, fmt.Errorf("%s", msg)
	}

	return changes, warnings, nil
}

//──[private helpers]─────────────────────────────────────────────────────

// hostEntrypoints returns every <machinesDir>/*/modules.nix that exists,
// sorted for deterministic iteration.
func hostEntrypoints(machinesDir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(machinesDir, "*", SelectionFile))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			files = append(files, m)
		}
	}
	sort.Strings(files)
	return files, nil
}

// scopedEntrypoints is entrypointsFor without the entrypoints of skipHosts.
func scopedEntrypoints(machinesDir, host string, skipHosts []string) ([]string, error) {
	all, err := entrypointsFor(machinesDir, host)
	if err != nil {
		return nil, err
	}
	skip := make(map[string]bool, len(skipHosts))
	for _, h := range skipHosts {
		skip[h] = true
	}
	var files []string
	for _, f := range all {
		if !skip[filepath.Base(filepath.Dir(f))] {
			files = append(files, f)
		}
	}
	return files, nil
}

// entrypointsFor returns the entrypoint file(s) a host-scoped call should
// act on: every <machinesDir>/*/modules.nix when host is "", or just
// <machinesDir>/<host>/modules.nix (if it exists) otherwise.
func entrypointsFor(machinesDir, host string) ([]string, error) {
	if host == "" {
		return hostEntrypoints(machinesDir)
	}
	file := filepath.Join(machinesDir, host, SelectionFile)
	if fi, err := os.Stat(file); err != nil || fi.IsDir() {
		if err != nil && os.IsNotExist(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	return []string{file}, nil
}

func shapeError(file string) error {
	return fmt.Errorf("%s does not have exactly one recognizable 'imports = [ ... ];' block", file)
}
