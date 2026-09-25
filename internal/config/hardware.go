package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// The three files a hardware folder (.local/hardware/<key>/) holds. Exported
// so the caller that creates them names them without repeating the strings.
const (
	HardwareConfigFile = "hardware-configuration.nix"
	BootFile           = "boot.nix"
	HardwareFile       = "hardware.nix"
)

// ValidateHardware checks that dir holds exactly HardwareConfigFile, BootFile
// and HardwareFile as regular files. Every missing or foreign entry is
// reported, sorted, in one error shaped like ValidateHost's.
func ValidateHardware(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	required := []string{HardwareConfigFile, BootFile, HardwareFile}
	seen := make(map[string]bool, len(entries))
	var problems []string

	for _, e := range entries {
		name := e.Name()
		switch name {
		case HardwareConfigFile, BootFile, HardwareFile:
			seen[name] = true
			info, statErr := os.Stat(filepath.Join(dir, name))
			if statErr != nil || !info.Mode().IsRegular() {
				problems = append(problems, name+" does not belong here")
			}
		default:
			problems = append(problems, name+" does not belong here")
		}
	}
	for _, name := range required {
		if !seen[name] {
			problems = append(problems, "missing "+name)
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
