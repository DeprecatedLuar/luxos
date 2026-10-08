package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	EnvironmentFile     = "environment"
	environmentTemplate = "starters/environment"

	gitignoreFile = ".gitignore"
	systemModules = "system"
)

var gitignoreLines = []string{"/modules/default.nix", "/modules/system", "/modules/local", "/local", "/.local/machines/*/modules/hardware-support"}

// Ensure brings CONFIG_DIR to the state a rebuild of host expects: .gitignore,
// the environment file, the synced framework modules, machine.nix, the host
// folder (validated and protected), this computer's hardware folder and the
// host's link to it, the mirror and local-module links, the machine.nix
// checks and the local link.
func Ensure(out *ui.Progress, p paths.Paths, host string) error {
	hostDir := filepath.Join(p.Machines, host)

	out.Printf("Ensuring .gitignore...\n")
	added, err := ensureGitignore(filepath.Join(p.Config, gitignoreFile), gitignoreLines)
	if err != nil {
		return err
	}
	for _, line := range added {
		out.Changef("  added: %s", line)
	}

	out.Printf("Ensuring environment file...\n")
	tmpl, err := templates.File(environmentTemplate)
	if err != nil {
		return err
	}
	envPath := filepath.Join(p.Config, EnvironmentFile)
	created, err := CreateFile(envPath, tmpl)
	if err != nil {
		return err
	}
	if created {
		out.Changef("  created: %s", envPath)
	}

	out.Printf("Syncing framework modules...\n")
	changes, err := Sync(filepath.Join(p.Modules, systemModules))
	if err != nil {
		return err
	}
	for _, c := range changes {
		line := fmt.Sprintf("%s: %s", c.Action, c.Path)
		if c.Action == ActionCreated {
			out.Changef("%s", line)
		} else {
			out.Warnf("%s", line)
		}
	}

	if err := ensureMachineFile(out, hostDir); err != nil {
		return err
	}

	out.Printf("Validating %s...\n", hostDir)
	if err := ValidateHost(hostDir); err != nil {
		return err
	}
	protected, err := ProtectHost(hostDir)
	if err != nil {
		return err
	}
	if protected {
		out.Changef("  protected: %s", filepath.Join(hostDir, plsDontTouchFile))
	}

	hwDir, err := EnsureHardware(out, p)
	if err != nil {
		return err
	}
	key := filepath.Base(hwDir)
	out.Printf("Ensuring local/modules/hardware-support -> .local/hardware/%s link...\n", key)
	if err := ensureHardwareLink(p.Machines, p.HardwareRoot, host, key); err != nil {
		return err
	}

	out.Printf("Ensuring %s's modules mirror...\n", host)
	if err := EnsureMirror(p.Machines, p.Modules, host); err != nil {
		return err
	}

	out.Printf("Ensuring modules/local -> .local/machines/%s/modules link...\n", host)
	if err := EnsureLocalModules(p.Machines, p.Modules, host); err != nil {
		return err
	}

	if err := checkMachineFile(filepath.Join(hostDir, MachineFile)); err != nil {
		return err
	}
	if _, err := readBaseChannel(filepath.Join(hostDir, MachineFile)); err != nil {
		return err
	}

	out.Printf("Ensuring local -> .local/machines/%s link...\n", host)
	if err := ensureLocalLink(p.Config, p.Machines, host); err != nil {
		return err
	}
	return out.Err()
}

// ActiveHost returns the active host's name, read from the modules/local link
// to <machines>/<host>/modules.
func ActiveHost(modulesDir string) (string, error) {
	real, err := filepath.EvalSymlinks(filepath.Join(modulesDir, localLinkName))
	if err != nil {
		return "", fmt.Errorf("no active host: modules/local is missing, run luxos rebuild first")
	}
	return filepath.Base(filepath.Dir(real)), nil
}

// CopyLockBack writes the staged flake.lock to hostLock when the bytes differ
// or hostLock is missing, and reports whether it did.
func CopyLockBack(stagedLock, hostLock string) (changed bool, err error) {
	staged, err := os.ReadFile(stagedLock)
	if err != nil {
		return false, err
	}
	host, err := os.ReadFile(hostLock)
	if err == nil && bytes.Equal(staged, host) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, WriteFile(hostLock, staged)
}
