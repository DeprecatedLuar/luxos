// Package config loads and validates machine.toml and channels.toml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// Required keys in machine.toml. Extend here as optional keys are added to
// the schema.
var machineRequiredKeys = []string{"timeZone", "locale", "stateVersion"}

// Machine is the decoded, validated content of a machine.toml file.
type Machine struct {
	TimeZone     string `toml:"timeZone"`
	Locale       string `toml:"locale"`
	StateVersion string `toml:"stateVersion"`
}

// LoadMachine decodes and validates the machine.toml at path. It returns
// warnings for unknown keys (besides the specifically rejected "hostName"
// and "users") and a single error covering every missing-required-key,
// "hostName" and "users" violation found.
func LoadMachine(path string) (Machine, []string, error) {
	var m Machine

	meta, err := toml.DecodeFile(path, &m)
	if err != nil {
		return Machine{}, nil, err
	}

	var errs []string
	for _, key := range machineRequiredKeys {
		if !meta.IsDefined(key) {
			errs = append(errs, fmt.Sprintf("Missing required key: %s", key))
		}
	}

	// "users" was machine.toml's second selection mechanism, stale now that
	// modules/users/ exists — a hard error naming the fix rather than the
	// generic unknown-key warning below.
	if meta.IsDefined("users") {
		errs = append(errs, "'users' is no longer a machine.toml key: remove users; select users in modules/default.nix")
	}

	// "hostName" was machine.toml's second source of the machine's identity
	// — the .local/<name> directory name is now the only one.
	if meta.IsDefined("hostName") {
		errs = append(errs, "'hostName' is no longer a machine.toml key: remove hostName; the directory name sets it")
	}

	if len(errs) > 0 {
		msg := fmt.Sprintf("Invalid TOML format in %s", path)
		for _, e := range errs {
			msg += "\n  - " + e
		}
		return Machine{}, nil, fmt.Errorf("%s", msg)
	}

	var warnings []string
	for _, key := range meta.Undecoded() {
		name := key.String()
		if name == "users" || name == "hostName" {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("unknown key '%s' in %s", name, path))
	}
	sort.Strings(warnings)

	return m, warnings, nil
}

// ResolveMachine resolves a machine name to its directory under localDir. A
// machine is a directory there holding a machine.toml.
func ResolveMachine(localDir, name string) (string, error) {
	dir := filepath.Join(localDir, name)
	machineToml := filepath.Join(dir, "machine.toml")

	if _, err := os.Stat(machineToml); err != nil {
		return "", fmt.Errorf("no machine.toml under %s\n  Pass --machine <name> if this machine was renamed or isn't named after $(hostname).", dir)
	}

	return dir, nil
}
