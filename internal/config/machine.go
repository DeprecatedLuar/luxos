// Package config loads and validates channels.toml, and validates the
// fixed .local/<host>/ folder layout: .plsdonttouch.nix, machine.nix,
// modules.nix, an optional flake.lock, and an optional modules/ directory.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Fixed entries under .local/<host>/ (L1). plsDontTouchFile, machineFile
// and selectionFile are required regular files (symlinks followed);
// lockFile is an optional regular file; localModulesDir is an optional
// directory. Nothing else may live there.
const (
	plsDontTouchFile = ".plsdonttouch.nix"
	machineFile      = "machine.nix"
	selectionFile    = "modules.nix"
	lockFile         = "flake.lock"
	localModulesDir  = "modules"

	plsDontTouchMode = 0444
)

// ResolveMachine resolves a machine name to its directory under localDir. A
// machine is a directory there holding modules.nix.
func ResolveMachine(localDir, name string) (string, error) {
	dir := filepath.Join(localDir, name)
	selection := filepath.Join(dir, selectionFile)

	if _, err := os.Stat(selection); err != nil {
		return "", fmt.Errorf("no %s under %s\n  Pass --machine <name> if this machine was renamed or isn't named after $(hostname).", selectionFile, dir)
	}

	return dir, nil
}

// ValidateMachine checks hostDir against the fixed host folder layout (L1).
// Every missing required file is reported as "missing <name>"; every entry
// that isn't one of the allowed names, or is the wrong type (a required
// file/flake.lock that isn't a regular file, or modules/ that isn't a
// directory), is reported as "<name> does not belong here". Problems are
// sorted and collected into one error; a clean layout returns nil.
func ValidateMachine(hostDir string) error {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		return err
	}

	requiredFiles := []string{plsDontTouchFile, machineFile, selectionFile}

	seen := make(map[string]bool, len(entries))
	var problems []string

	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(hostDir, name)

		switch name {
		case plsDontTouchFile, machineFile, selectionFile, lockFile:
			seen[name] = true
			info, statErr := os.Stat(path)
			if statErr != nil || info.IsDir() {
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
	msg := fmt.Sprintf("invalid machine folder %s", hostDir)
	for _, p := range problems {
		msg += "\n  - " + p
	}
	return fmt.Errorf("%s", msg)
}

// ProtectMachine chmods hostDir/.plsdonttouch.nix to plsDontTouchMode if its
// mode differs, reporting whether it changed.
func ProtectMachine(hostDir string) (bool, error) {
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
