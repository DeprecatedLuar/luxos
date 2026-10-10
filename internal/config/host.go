package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// plsDontTouchFile, MachineFile and SelectionFile are required regular files
// (symlinks followed); lockFile is an optional regular file; localModulesDir
// and SettingsDir are optional directories. Nothing else may live there.
// MachineFile is exported so the caller that creates it from a template names
// it without repeating the string.
const (
	plsDontTouchFile = ".plsdonttouch.nix"
	MachineFile      = "machine.nix"
	SelectionFile    = "modules.nix"
	lockFile         = "flake.lock"
	localModulesDir  = "modules"
	// SettingsDir holds the host's values for its configurable units.
	SettingsDir = "settings"
	settingsExt = ".nix"

	plsDontTouchMode = 0444

	machineTemplate = "machine.nix.tmpl"
)

// MachineValues fill the machine.nix template.
type MachineValues struct {
	Channel, Timezone, Locale, Keyboard string
}

// MachineDefaults fill a machine.nix rebuild creates, and stand in for values
// setup cannot detect.
var MachineDefaults = MachineValues{Channel: "nixos-25.11", Timezone: "UTC", Locale: "en_US.UTF-8", Keyboard: "us"}

func RenderMachine(v MachineValues) ([]byte, error) {
	return templates.Render(machineTemplate, v)
}

// ErrNoMachine is a host folder without modules.nix.
var ErrNoMachine = errors.New("no " + SelectionFile)

// A host is a directory there holding modules.nix.
func ResolveHost(machinesDir, name string) (string, error) {
	dir := filepath.Join(machinesDir, name)
	selection := filepath.Join(dir, SelectionFile)

	if _, err := os.Stat(selection); err != nil {
		old := filepath.Join(filepath.Dir(machinesDir), name)
		if _, oldErr := os.Stat(filepath.Join(old, SelectionFile)); oldErr == nil {
			return "", fmt.Errorf("host folder %s must move to %s:\n  mv %s %s", old, dir, old, dir)
		}
		return "", fmt.Errorf("%w under %s", ErrNoMachine, dir)
	}

	return dir, nil
}

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

	requiredFiles := []string{plsDontTouchFile, MachineFile, SelectionFile}

	seen := make(map[string]bool, len(entries))
	var problems []string

	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(hostDir, name)

		switch name {
		case plsDontTouchFile, MachineFile, SelectionFile, lockFile:
			seen[name] = true
			info, statErr := os.Stat(path)
			if statErr != nil || info.IsDir() {
				problems = append(problems, name+" does not belong here")
			}
		case localModulesDir, SettingsDir:
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

	return problemsError("invalid host folder "+hostDir, problems)
}

// SettingsPath is hostDir's settings file for unit ("eduardo/git" → settings/eduardo/git.nix).
func SettingsPath(hostDir, unit string) string {
	return SettingsFolder(hostDir, unit) + settingsExt
}

// SettingsFolder is where the settings of unit's submodules live when unit is a bundle.
func SettingsFolder(hostDir, unit string) string {
	return filepath.Join(hostDir, SettingsDir, filepath.FromSlash(unit))
}

// problemsError lists problems, sorted, under title; none is nil.
func problemsError(title string, problems []string) error {
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New(title + "\n  - " + strings.Join(problems, "\n  - "))
}

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

// ensureMachineFile writes the host's machine.nix from the starter when it is missing.
func ensureMachineFile(out *ui.Progress, hostDir string) error {
	tmpl, err := RenderMachine(MachineDefaults)
	if err != nil {
		return err
	}
	path := filepath.Join(hostDir, MachineFile)
	created, err := CreateFile(path, tmpl)
	if err != nil {
		return err
	}
	if created {
		out.Changef("  created: %s", path)
	}
	return nil
}

func readBaseChannel(path string) (string, error) {
	url, _, err := nix.BaseChannel(path)
	if errors.Is(err, nix.ErrNoBaseChannel) {
		return "", fmt.Errorf("%s: %w\n  add: %s", path, err, baseChannelHint())
	}
	return url, err
}

func baseChannelHint() string {
	tmpl, err := RenderMachine(MachineDefaults)
	if err == nil {
		for _, line := range strings.Split(string(tmpl), "\n") {
			if strings.Contains(line, nix.BaseChannelInput+".url") {
				return strings.TrimSpace(line)
			}
		}
	}
	return "luxos.inputs.nixpkgs.url = \"<flake url>\";"
}

// checkMachineFile fails when the machine.nix at path holds any static or
// dynamic path literal.
func checkMachineFile(path string) error {
	static, dynamic, err := nix.PathsAndDynamic(path)
	if err != nil {
		return err
	}
	offenders := append(static, dynamic...)
	if len(offenders) == 0 {
		return nil
	}
	return fmt.Errorf("%s holds path literals or imports: %s\n"+
		"paths in machine.nix resolve against the staged copy at /etc/nixos/config/, not the host folder, "+
		"and module selection belongs in modules.nix; add a module with:\n  luxos module enable <name>",
		path, strings.Join(offenders, ", "))
}
