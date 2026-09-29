package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Exported so callers name them without repeating the strings.
const (
	DefaultFile        = "default.nix"
	HardwareConfigFile = "hardware-configuration.nix"
	BootFile           = "boot.nix"
	HardwareFile       = "hardware.nix"

	HardwareUnitName = "hardware-support"
	HardwareUnitPath = "local/" + HardwareUnitName
)

// Any other entry is allowed. Every problem is reported, sorted, in one error
// shaped like ValidateHost's: "missing <name>" or "<name> is not a regular file".
func ValidateHardware(dir string) error {
	var problems []string
	for _, name := range []string{DefaultFile, HardwareConfigFile, BootFile, HardwareFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		switch {
		case os.IsNotExist(err):
			problems = append(problems, "missing "+name)
		case err != nil || !info.Mode().IsRegular():
			problems = append(problems, name+" is not a regular file")
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	msg := fmt.Sprintf("invalid hardware folder %s", dir)
	for _, p := range problems {
		msg += "\n  - " + p
	}
	return fmt.Errorf("%s", msg)
}
