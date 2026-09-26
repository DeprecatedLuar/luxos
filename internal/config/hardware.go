package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// The four files a hardware folder (.local/hardware/<key>/) must hold, and the
// unit path the folder is reached by once linked into a host's local modules.
// Exported so callers name them without repeating the strings.
const (
	DefaultFile        = "default.nix"
	HardwareConfigFile = "hardware-configuration.nix"
	BootFile           = "boot.nix"
	HardwareFile       = "hardware.nix"

	HardwareUnitPath = "local/hardware"
)

// ValidateHardware checks that dir holds DefaultFile, HardwareConfigFile,
// BootFile and HardwareFile as regular files (symlinks followed). Any other
// entry is allowed. Every problem is reported, sorted, in one error shaped
// like ValidateHost's: "missing <name>" or "<name> is not a regular file".
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
