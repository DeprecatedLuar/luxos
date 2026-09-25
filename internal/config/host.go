// Package config validates the fixed .local/machines/<host>/ folder layout: .plsdonttouch.nix, machine.nix,
// modules.nix, an optional flake.lock, an optional modules/ directory and the
// hardware symlink to this computer's hardware folder.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Fixed entries under .local/machines/<host>/ (L1). plsDontTouchFile, MachineFile
// and selectionFile are required regular files (symlinks followed);
// lockFile is an optional regular file; localModulesDir is an optional
// directory; hardwareLink is an optional symlink (target not checked). Nothing else may live there. MachineFile is exported so the
// caller that creates it from a template names it without repeating the
// string.
const (
	plsDontTouchFile = ".plsdonttouch.nix"
	MachineFile      = "machine.nix"
	selectionFile    = "modules.nix"
	lockFile         = "flake.lock"
	localModulesDir  = "modules"
	hardwareLink     = "hardware"

	plsDontTouchMode = 0444
)

// ResolveHost resolves a host name to its directory under machinesDir. A
// host is a directory there holding modules.nix.
func ResolveHost(machinesDir, name string) (string, error) {
	dir := filepath.Join(machinesDir, name)
	selection := filepath.Join(dir, selectionFile)

	if _, err := os.Stat(selection); err != nil {
		old := filepath.Join(filepath.Dir(machinesDir), name)
		if _, oldErr := os.Stat(filepath.Join(old, selectionFile)); oldErr == nil {
			return "", fmt.Errorf("host folder %s must move to %s:\n  mv %s %s", old, dir, old, dir)
		}
		return "", fmt.Errorf("no %s under %s\n  Pass --machine <name> if this host was renamed or isn't named after $(hostname).", selectionFile, dir)
	}

	return dir, nil
}

// ValidateHost checks hostDir against the fixed host folder layout (L1).
// Every missing required file is reported as "missing <name>"; every entry
// that isn't one of the allowed names, or is the wrong type (a required
// file/flake.lock that isn't a regular file, or modules/ that isn't a
// directory), is reported as "<name> does not belong here". Problems are
// sorted and collected into one error; a clean layout returns nil.
func ValidateHost(hostDir string) error {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		return err
	}

	requiredFiles := []string{plsDontTouchFile, MachineFile, selectionFile}

	seen := make(map[string]bool, len(entries))
	var problems []string

	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(hostDir, name)

		switch name {
		case plsDontTouchFile, MachineFile, selectionFile, lockFile:
			seen[name] = true
			info, statErr := os.Stat(path)
			if statErr != nil || info.IsDir() {
				problems = append(problems, name+" does not belong here")
			}
		case hardwareLink:
			seen[name] = true
			info, statErr := os.Lstat(path)
			if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
				problems = append(problems, name+" does not belong here")
			}
		case localModulesDir:
			seen[name] = true
			info, statErr := os.Stat(path)
			if statErr != nil || !info.IsDir() {
				problems = append(problems, name+" does not belong here")
			}
		default:
			problems = append(problems, name+" does not belong here")
		}
	}

	for _, name := range requiredFiles {
		if !seen[name] {
			problems = append(problems, "missing "+name)
		}
	}

	if len(problems) == 0 {
		return nil
	}

	sort.Strings(problems)
	msg := fmt.Sprintf("invalid host folder %s", hostDir)
	for _, p := range problems {
		msg += "\n  - " + p
	}
	return fmt.Errorf("%s", msg)
}

// ProtectHost chmods hostDir/.plsdonttouch.nix to plsDontTouchMode if its
// mode differs, reporting whether it changed.
func ProtectHost(hostDir string) (bool, error) {
	path := filepath.Join(hostDir, plsDontTouchFile)

	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Mode().Perm() == plsDontTouchMode {
		return false, nil
	}
	if err := os.Chmod(path, plsDontTouchMode); err != nil {
		return false, err
	}
	return true, nil
}
