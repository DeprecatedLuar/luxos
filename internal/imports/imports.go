// Package imports is the sole reader/writer of host entrypoints
// (implementation-plan.md #18): .local/machines/<host>/modules.nix files, the
// host's selection (L4). The block's syntax (recognizing, reading and
// editing the single "imports = [ ... ];" block) lives in internal/nixsrc;
// this package owns which hosts and names. A file with more than one
// recognizable block, or one written some other way (computed imports, a
// single-line block with items), is refused by the writers (Add/Remove) and
// skipped, with a warning, by the read paths that loop every host
// (Importers/Retarget/Heal).
//
// Path convention used throughout this file's public API: a "path" is
// relative to CONFIG_DIR/modules with no leading "./" (the same shape
// units.Resolve returns) — List strips it on read, Add/Retarget add it
// back on write.
package imports

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/nixsrc"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

// entrypointName is the file name every host's modules selection lives in,
// found under <LOCAL_DIR>/*/modules.nix (L4).
const entrypointName = "modules.nix"

// localPrefix marks an import path as belonging to the active host's own
// local modules (L5/L6), e.g. "local/foo.nix".
const localPrefix = "local/"

// localModulesSubdir is the directory name under <LOCAL_DIR>/<host>/ that
// holds that host's local modules (L5).
const localModulesSubdir = "modules"

// Change is one rewrite made by Retarget or Heal. New == "" means the line
// was removed.
type Change struct {
	File, Old, New string
}

// List returns the active import paths in file, bare (no leading "./"),
// in file order. Hard error if file doesn't have exactly one recognizable
// imports block.
func List(file string) ([]string, error) {
	paths, ok, err := nixsrc.ListImports(file)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, shapeError(file)
	}
	return paths, nil
}

// Add appends one import line for path (leading "./" optional) to file's
// single imports block. A no-op if path is already present.
func Add(file, path string) error {
	return nixsrc.AddImport(file, path)
}

// Remove deletes every import line in file's single block whose path
// equals path (bare, "./" optional). A no-op (no write) if none match.
func Remove(file, path string) error {
	return nixsrc.RemoveImport(file, path)
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
		paths, ok, err := nixsrc.ListImports(file)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for _, p := range paths {
			if units.NameFromPath(p) == name {
				out = append(out, file)
				break
			}
		}
	}
	return out, nil
}

// Retarget is the writer of host entrypoints (#18): rewrite every import
// line whose name matches name to newPath, or delete it when newPath is
// "". host == "" reaches every <machinesDir>/*/modules.nix (the cross-host
// case, for a shared unit); otherwise only <machinesDir>/<host>/modules.nix is
// touched (the local-unit case, L7). Returns every change made; a no-op
// line (already at newPath) is left untouched and unreported, so a second
// run returns no changes. A host whose file isn't recognizable is warned
// about and left untouched. The entrypoints of skipHosts are never rewritten.
func Retarget(machinesDir, name, newPath, host string, skipHosts []string) ([]Change, error) {
	changes, _, err := retarget(machinesDir, name, newPath, host, skipHosts)
	return changes, err
}

func retarget(machinesDir, name, newPath, host string, skipHosts []string) ([]Change, []string, error) {
	all, err := entrypointsFor(machinesDir, host)
	if err != nil {
		return nil, nil, err
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

	match := func(p string) bool { return units.NameFromPath(p) == name }
	newPath = strings.TrimPrefix(newPath, "./")

	var changes []Change
	var warnings []string

	for _, file := range files {
		old, ok, err := nixsrc.RetargetImports(file, match, newPath)
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

// Heal rewrites every broken import line in every host's entrypoint,
// per host (L7): each host's selection lines are checked and resolved
// against that host's own unit set (shared units plus that host's own
// local/ units, per units.Walk(modulesDir, <machinesDir>/<host>/modules)).
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
// A host whose own units.Walk fails errors the whole call when it's
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

		us, err := units.Walk(modulesDir, filepath.Join(machinesDir, host, localModulesSubdir))
		if err != nil {
			if host == activeHost {
				return nil, nil, err
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s", host, err))
			continue
		}

		paths, ok, err := nixsrc.ListImports(file)
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
			if rest, cut := strings.CutPrefix(itPath, localPrefix); cut {
				fullPath = filepath.Join(machinesDir, host, localModulesSubdir, rest)
			} else {
				fullPath = filepath.Join(modulesDir, itPath)
			}
			if _, err := os.Stat(fullPath); err == nil {
				continue
			}

			name := units.NameFromPath(itPath)
			if resolved, ok := units.Resolve(us, name); ok {
				if strings.HasPrefix(resolved, localPrefix) {
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
					if err := Remove(file, itPath); err != nil {
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
	matches, err := filepath.Glob(filepath.Join(machinesDir, "*", entrypointName))
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

// entrypointsFor returns the entrypoint file(s) a host-scoped call should
// act on: every <machinesDir>/*/modules.nix when host is "", or just
// <machinesDir>/<host>/modules.nix (if it exists) otherwise.
func entrypointsFor(machinesDir, host string) ([]string, error) {
	if host == "" {
		return hostEntrypoints(machinesDir)
	}
	file := filepath.Join(machinesDir, host, entrypointName)
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

// shapeError is the standard "shape not recognized" error for file.
func shapeError(file string) error {
	return fmt.Errorf("%s does not have exactly one recognizable 'imports = [ ... ];' block", file)
}
