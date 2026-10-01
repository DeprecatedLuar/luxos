package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	EnvironmentFile     = "environment"
	environmentTemplate = "starters/environment"

	gitignoreFile = ".gitignore"
	systemModules = "system"
)

var gitignoreLines = []string{"/modules/default.nix", "/modules/system", "/modules/local", "/local", "/.local/machines/*/modules/hardware-support"}

// progress prints to w and keeps the first write error, so a broken output
// stream is reported rather than dropped while Ensure keeps sequencing.
type progress struct {
	w   io.Writer
	err error
}

func (o *progress) printf(format string, a ...any) {
	if o.err == nil {
		_, o.err = fmt.Fprintf(o.w, format, a...)
	}
}

// Ensure brings CONFIG_DIR to the state a rebuild of host expects: .gitignore,
// the environment file, the synced framework modules, machine.nix, the host
// folder (validated and protected), this computer's hardware folder and the
// host's link to it, the mirror and local-module links, the machine.nix
// checks and the local link.
func Ensure(w io.Writer, p paths.Paths, host string) error {
	hostDir := filepath.Join(p.Machines, host)
	out := &progress{w: w}

	out.printf("Ensuring .gitignore...\n")
	added, err := ensureGitignore(filepath.Join(p.Config, gitignoreFile), gitignoreLines)
	if err != nil {
		return err
	}
	for _, line := range added {
		out.printf("  added: %s\n", line)
	}

	out.printf("Ensuring environment file...\n")
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
		out.printf("  created: %s\n", envPath)
	}

	out.printf("Syncing framework modules...\n")
	changes, err := Sync(filepath.Join(p.Modules, systemModules))
	if err != nil {
		return err
	}
	for _, c := range changes {
		line := fmt.Sprintf("%s: %s", c.Action, c.Path)
		if c.Action == ActionCreated {
			out.printf("%s\n", line)
		} else {
			out.printf("Warning: %s\n", line)
		}
	}

	if err := ensureMachineFile(out, hostDir); err != nil {
		return err
	}

	out.printf("Validating %s...\n", hostDir)
	if err := ValidateHost(hostDir); err != nil {
		return err
	}
	protected, err := ProtectHost(hostDir)
	if err != nil {
		return err
	}
	if protected {
		out.printf("  protected: %s\n", filepath.Join(hostDir, plsDontTouchFile))
	}

	hwDir, err := ensureHardware(out, p)
	if err != nil {
		return err
	}
	key := filepath.Base(hwDir)
	out.printf("Ensuring local/modules/hardware-support -> .local/hardware/%s link...\n", key)
	if err := ensureHardwareLink(p.Machines, p.HardwareRoot, host, key); err != nil {
		return err
	}

	out.printf("Ensuring %s's modules mirror...\n", host)
	if err := EnsureMirror(p.Machines, p.Modules, host); err != nil {
		return err
	}

	out.printf("Ensuring modules/local -> .local/machines/%s/modules link...\n", host)
	if err := EnsureLocalModules(p.Machines, p.Modules, host); err != nil {
		return err
	}

	if err := checkMachineFile(filepath.Join(hostDir, MachineFile)); err != nil {
		return err
	}
	if _, err := readBaseChannel(filepath.Join(hostDir, MachineFile)); err != nil {
		return err
	}

	out.printf("Ensuring local -> .local/machines/%s link...\n", host)
	if err := ensureLocalLink(p.Config, p.Machines, host); err != nil {
		return err
	}
	return out.err
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
